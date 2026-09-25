// Package session builds a live connection to one Argo server from one named
// profile: the config merge, the owned port-forward, and the HTTP client on
// top of it.
//
// It exists because a profile is chosen more than once. The picker switches
// profile while the program runs, and every switch has to build the same
// things startup built. The root model must not know what a kubectl
// port-forward is, so it holds the Connector defined here behind the narrow
// app.Connector interface and asks it for a connection by name.
package session

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ficaa1/argo-tui/internal/app"
	"github.com/ficaa1/argo-tui/internal/argo"
	"github.com/ficaa1/argo-tui/internal/buildinfo"
	"github.com/ficaa1/argo-tui/internal/config"
	"github.com/ficaa1/argo-tui/internal/diagnostics"
	"github.com/ficaa1/argo-tui/internal/portforward"
	"github.com/ficaa1/argo-tui/internal/ui/profiles"
)

// readyTimeout bounds the wait for a port-forward to announce its local port.
// Past it the profile is reported as unreachable rather than hanging the
// picker, because the reader has another profile they could choose instead.
const readyTimeout = 30 * time.Second

// Options is the command-line layer. Every field is applied on top of whatever
// profile is chosen, so they keep their meaning across a switch.
type Options struct {
	ConfigPath            string
	Server                string
	Namespace             string
	TokenFile             string
	CAFile                string
	RefreshInterval       time.Duration
	InsecureSkipTLSVerify bool
	Debug                 bool
	// Skin is the --skin flag, which outranks every skin in the file.
	Skin string
	// Skins is the set of valid skin names. Every skin the file names is
	// checked against it when the file is read.
	Skins []string
	// RedactValues is the --redact-values flag, passed through to every
	// profile's configuration.
	RedactValues bool
	// Diagnostics receives sanitized forwarding lifecycle lines when Debug is
	// set. Nil silences them.
	Diagnostics io.Writer
}

// Connector builds connections from one config file.
//
// The file is read once, at construction. A profile added to it while the
// program runs is not offered until the next run: re-reading it on every
// switch would let an edit halfway through saving break a switch that was
// working a second earlier.
type Connector struct {
	opts Options
	// path is the resolved config file, or empty when none was found.
	path string
	// data is the raw file, empty when there is none.
	data []byte

	items   []profiles.Item
	current string
	// listErr says why the file yielded no profiles.
	listErr string

	// mu guards live, the connection this connector last handed out. The
	// entrypoint closes it on the way out, and Connect closes nothing: the
	// root closes the connection it is replacing before it asks for the next.
	mu   sync.Mutex
	live *app.Connection
}

// NewConnector reads the config file and lists the profiles in it. A missing
// file is not an error: the picker opens empty and says where to write one.
func NewConnector(opts Options) (*Connector, error) {
	c := &Connector{opts: opts, path: opts.ConfigPath}
	if c.path == "" {
		c.path, _ = config.DefaultConfigPath()
	}
	if c.path != "" {
		data, err := os.ReadFile(c.path)
		switch {
		case err == nil:
			c.data = data
		case os.IsNotExist(err):
			// Nothing to read. The picker names the path so the reader knows
			// where the file they do not have should go.
		default:
			return nil, fmt.Errorf("read config: %w", err)
		}
	}
	// A misspelled skin is a startup error, including one on a profile the
	// reader has not chosen yet: finding out at the switch would mean a
	// session that cannot be drawn the way the file asks.
	if err := config.ValidateSkins(c.data, opts.Skins); err != nil {
		return nil, err
	}
	summaries, current, err := config.ListProfiles(c.data)
	if err != nil {
		c.listErr = err.Error()
	}
	c.current = current
	for _, s := range summaries {
		c.items = append(c.items, profiles.Item{Name: s.Name, Server: s.Server, Namespace: s.Namespace})
	}
	return c, nil
}

// ProfileList is what the root needs to render the picker.
func (c *Connector) ProfileList() app.ProfileList {
	return app.ProfileList{Items: c.items, ConfigPath: c.path, Current: c.current, Err: c.listErr}
}

// Skin is the skin to draw in before a profile is chosen: the flag, else the
// file's top-level skin, else the default. A connected profile may name its
// own, which arrives with its connection.
func (c *Connector) Skin() string {
	if c.opts.Skin != "" {
		return c.opts.Skin
	}
	if s := config.FileSkin(c.data); s != "" {
		return s
	}
	return config.DefaultSkin
}

// HasProfiles reports whether the config file named any profile.
func (c *Connector) HasProfiles() bool { return len(c.items) > 0 }

// Close releases the connection this connector last handed out. The entrypoint
// defers it, so a port-forward is never left running by an exit path the model
// did not take.
func (c *Connector) Close() {
	c.mu.Lock()
	live := c.live
	c.live = nil
	c.mu.Unlock()
	if live != nil && live.Close != nil {
		live.Close()
	}
}

// Connect builds a connection for one profile. An empty name uses only the
// command-line options, which is how --server without a profile connects.
//
// It blocks: starting a port-forward and waiting for it to bind a local port
// takes seconds. Every caller runs it off the update loop.
func (c *Connector) Connect(ctx context.Context, profile string) (*app.Connection, error) {
	cfg, err := config.Load(c.data, config.Options{
		ConfigPath:            c.path,
		Profile:               profile,
		Server:                c.opts.Server,
		Namespace:             c.opts.Namespace,
		TokenFile:             c.opts.TokenFile,
		CAFile:                c.opts.CAFile,
		RefreshInterval:       c.opts.RefreshInterval,
		InsecureSkipTLSVerify: c.opts.InsecureSkipTLSVerify,
		Debug:                 c.opts.Debug,
		Skin:                  c.opts.Skin,
		Skins:                 c.opts.Skins,
		RedactValues:          c.opts.RedactValues,
	})
	if err != nil {
		return nil, err
	}

	// The token is read per request rather than captured here, so a rotated
	// token is picked up without a reconnection.
	tokenFn := func() (string, error) {
		if cfg.TokenFile != "" {
			b, err := os.ReadFile(cfg.TokenFile)
			return strings.TrimSuffix(string(b), "\n"), err
		}
		return os.Getenv(cfg.TokenEnv), nil
	}

	serverURL := cfg.Server
	var forwarder *portforward.Manager
	var states chan app.ConnectionStateMsg
	var resolveServer func() string
	if cfg.Target.Service != "" {
		forwarder, states, serverURL, err = c.startForward(ctx, cfg)
		if err != nil {
			return nil, err
		}
		// resolveServer keeps the transport pointed at the currently owned
		// local port. The configured scheme, path prefix and TLS material are
		// fixed at construction and are not affected by recovery.
		resolveServer = func() string { return forwardEndpoint(cfg.Server, forwarder.Endpoint()) }
	}

	client, err := argo.NewClient(argo.Options{
		Server:                serverURL,
		TokenFn:               tokenFn,
		TokenSource:           "configured credential source",
		CAFile:                cfg.CAFile,
		InsecureSkipTLSVerify: cfg.InsecureSkipTLSVerify,
		ResolveServer:         resolveServer,
		UserAgent:             buildinfo.UserAgent(),
	})
	if err != nil {
		if forwarder != nil {
			forwarder.Close()
		}
		return nil, err
	}

	var once sync.Once
	conn := &app.Connection{
		Reader:      client,
		Profile:     cfg.ProfileName,
		Server:      cfg.Server,
		Namespace:   cfg.Namespace,
		WebURL:      cfg.WebURL,
		PipeCommand: cfg.PipeCommand,
		Skin:        cfg.Skin,
		Redact:      cfg.RedactValues,
		Namespaces:  cfg.Namespaces,
		Interval:    cfg.RefreshInterval,
		States:      states,
	}
	if forwarder != nil {
		// Close is idempotent: the root closes a connection it replaces, and
		// the entrypoint closes whatever is live on the way out. Those are the
		// same connection whenever the program exits without a switch.
		conn.Close = func() { once.Do(forwarder.Close) }
	}
	c.mu.Lock()
	c.live = conn
	c.mu.Unlock()
	return conn, nil
}

// startForward owns the kubectl port-forward for one profile and returns the
// endpoint it bound.
//
// LocalPort stays empty so kubectl binds an ephemeral loopback port. Nothing
// is ever sent to a guessed or reused port: the endpoint comes only from the
// readiness line of the process we own, and it is re-read per request because
// every recovery binds a new port.
func (c *Connector) startForward(ctx context.Context, cfg config.Config) (*portforward.Manager, chan app.ConnectionStateMsg, string, error) {
	fwd, err := portforward.New(portforward.Target{
		Context:    cfg.Target.Context,
		Namespace:  cfg.Target.Namespace,
		Service:    cfg.Target.Service,
		RemotePort: strconv.Itoa(cfg.Target.RemotePort),
		LocalPort:  "",
	})
	if err != nil {
		return nil, nil, "", fmt.Errorf("port-forward: %w", err)
	}
	fwd.Start(context.Background())

	// Drain lifecycle events from the moment forwarding starts, before waiting
	// for readiness: a stalled consumer must never be able to hold back the
	// recovery loop. The channel buffers the transitions until the update loop
	// starts reading them.
	states := make(chan app.ConnectionStateMsg, 32)
	var sink *diagnostics.Sink
	if c.opts.Debug && c.opts.Diagnostics != nil {
		sink = diagnostics.New(c.opts.Diagnostics)
	}
	go func() {
		defer close(states)
		for e := range fwd.Events() {
			if sink != nil {
				sink.Emit(diagnostics.StageForward, string(e.State), e.Attempt-1, e.State == portforward.StateLost, 0, "")
			}
			switch e.State {
			// Only a change in whether the transport can carry a request
			// reaches the model. StateStarting sits between two of those,
			// and StateWarning is an error line from a forward that still
			// serves: reporting either as a lost connection would block
			// actions while every request still succeeds.
			case portforward.StateReady, portforward.StateLost, portforward.StateFailed, portforward.StateStopped:
			default:
				continue
			}
			msg := app.ConnectionStateMsg{Ready: e.State == portforward.StateReady}
			if msg.Ready {
				msg.Target = forwardEndpoint(cfg.Server, fwd.Endpoint())
			}
			select {
			case states <- msg:
			default: // never block forwarding on the UI
			}
		}
	}()

	readyCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	err = fwd.Ready(readyCtx)
	cancel()
	if err != nil {
		fwd.Close()
		return nil, nil, "", fmt.Errorf("port-forward readiness: %w", err)
	}
	endpoint := forwardEndpoint(cfg.Server, fwd.Endpoint())
	if endpoint == "" {
		fwd.Close()
		return nil, nil, "", fmt.Errorf("port-forward announced readiness without an endpoint")
	}
	return fwd, states, endpoint, nil
}

// forwardEndpoint combines the announced loopback address of the owned
// port-forward with the scheme and path prefix of the configured server. The
// forward moves the transport to a new host and port; it must not silently
// downgrade a configured https endpoint to plain http, and it must not discard
// a configured base path.
func forwardEndpoint(configured, announced string) string {
	if announced == "" {
		return ""
	}
	a, err := url.Parse(announced)
	if err != nil || a.Host == "" {
		return ""
	}
	cu, err := url.Parse(configured)
	if err != nil || cu.Scheme == "" {
		return announced
	}
	out := url.URL{Scheme: cu.Scheme, Host: a.Host, Path: strings.TrimSuffix(cu.Path, "/")}
	return out.String()
}
