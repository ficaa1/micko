package session

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ficaa1/micko/internal/argo"
	"github.com/ficaa1/micko/internal/buildinfo"
	"github.com/ficaa1/micko/internal/config"
	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/kube"
	"github.com/ficaa1/micko/internal/ui/profiles"
)

// probeName is the profile name a draft is validated under before it has one.
const probeName = "new"

// KubeContexts lists the kubeconfig's contexts for the setup wizard.
func (c *Connector) KubeContexts(ctx context.Context) ([]string, string, error) {
	return kube.Contexts(ctx, c.kubectl)
}

// ArgoServers lists the Argo Server services in kubeContext.
func (c *Connector) ArgoServers(ctx context.Context, kubeContext string) ([]profiles.Server, error) {
	found, err := kube.ArgoServers(ctx, c.kubectl, kubeContext)
	servers := make([]profiles.Server, len(found))
	for i, s := range found {
		servers[i] = profiles.Server{Namespace: s.Namespace, Name: s.Name, Port: s.Port}
	}
	return servers, err
}

// Namespaces lists kubeContext's namespaces.
func (c *Connector) Namespaces(ctx context.Context, kubeContext string) ([]string, error) {
	return kube.Namespaces(ctx, c.kubectl, kubeContext)
}

// Probe forwards to the draft's server and lists one workflow, first over
// HTTPS and then, if the server speaks plain HTTP, over HTTP. A draft with no
// token source is tried without one, which is how server auth mode is found.
func (c *Connector) Probe(ctx context.Context, d profiles.Draft) profiles.Check {
	d.ServerAuth = d.TokenEnv == "" && d.TokenFile == ""
	if d.TokenEnv != "" && os.Getenv(d.TokenEnv) == "" {
		return profiles.Check{Outcome: profiles.NeedsToken, Draft: d, Detail: "$" + d.TokenEnv + " is not set in micko's environment"}
	}
	cfg, err := draftConfig(d)
	if err != nil {
		return profiles.Check{Outcome: profiles.Failed, Draft: d, Detail: err.Error()}
	}
	endpoint, closeForward, err := c.forward(ctx, cfg)
	if err != nil {
		return profiles.Check{Outcome: profiles.Failed, Draft: d, Detail: err.Error()}
	}
	defer closeForward()

	err = c.listOne(ctx, cfg, endpoint)
	if err != nil && !d.HTTP && strings.Contains(err.Error(), "HTTP response to HTTPS client") {
		d.HTTP = true
		if cfg, err = draftConfig(d); err == nil {
			err = c.listOne(ctx, cfg, endpoint)
		}
	}
	var apiErr *core.APIError
	errors.As(err, &apiErr)
	switch {
	case err == nil:
		return profiles.Check{Outcome: profiles.Reachable, Draft: d}
	case apiErr != nil && apiErr.Kind == core.ErrUnauthenticated:
		detail := ""
		if !d.ServerAuth {
			detail = "the server rejected the token from " + tokenFrom(d)
		}
		return profiles.Check{Outcome: profiles.NeedsToken, Draft: d, Detail: detail}
	case untrusted(err):
		detail := "the certificate is signed by an unknown authority"
		var host x509.HostnameError
		if errors.As(err, &host) {
			detail = "the certificate does not name the forwarded address"
		}
		return profiles.Check{Outcome: profiles.Untrusted, Draft: d, Detail: detail}
	case apiErr != nil && apiErr.Kind == core.ErrForbidden:
		return profiles.Check{Outcome: profiles.Failed, Draft: d,
			Detail: "the server refused to list workflows in " + d.Namespace + ": " + apiErr.Message}
	default:
		return profiles.Check{Outcome: profiles.Failed, Draft: d, Detail: err.Error()}
	}
}

// Save appends the draft to the config file under name and re-reads the file,
// so the new profile can be connected to at once.
func (c *Connector) Save(name string, d profiles.Draft) ([]profiles.Item, error) {
	if c.path == "" {
		return nil, fmt.Errorf("no config path: set --config or $XDG_CONFIG_HOME")
	}
	if err := config.AddProfile(c.path, name, draftProfile(d)); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	summaries, current, err := config.ListProfiles(data)
	if err != nil {
		return nil, err
	}
	items := make([]profiles.Item, len(summaries))
	for i, s := range summaries {
		items[i] = profiles.Item{Name: s.Name, Server: s.Server, Namespace: s.Namespace}
	}
	c.mu.Lock()
	c.data, c.items, c.current, c.listErr = data, items, current, ""
	c.mu.Unlock()
	return items, nil
}

func (c *Connector) listOne(ctx context.Context, cfg config.Config, endpoint string) error {
	tokenFn := func() (string, error) { return "", nil }
	switch {
	case cfg.TokenFile != "":
		tokenFn = func() (string, error) {
			b, err := os.ReadFile(cfg.TokenFile)
			return strings.TrimSpace(string(b)), err
		}
	case cfg.TokenEnv != "":
		tokenFn = func() (string, error) { return os.Getenv(cfg.TokenEnv), nil }
	}
	client, err := argo.NewClient(argo.Options{
		Server:                forwardEndpoint(cfg.Server, endpoint),
		TokenFn:               tokenFn,
		TokenSource:           "configured credential source",
		CAFile:                cfg.CAFile,
		InsecureSkipTLSVerify: cfg.InsecureSkipTLSVerify,
		UserAgent:             buildinfo.UserAgent(),
	})
	if err != nil {
		return err
	}
	_, err = client.List(ctx, core.Query{Namespace: cfg.Namespace, Limit: 1})
	return err
}

// untrusted reports a certificate the client could not verify.
func untrusted(err error) bool {
	var authority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var host x509.HostnameError
	return errors.As(err, &authority) || errors.As(err, &invalid) || errors.As(err, &host)
}

func tokenFrom(d profiles.Draft) string {
	if d.TokenFile != "" {
		return d.TokenFile
	}
	return "$" + d.TokenEnv
}

// draftProfile is the profile a draft is saved as.
func draftProfile(d profiles.Draft) config.Profile {
	scheme := "https"
	if d.HTTP {
		scheme = "http"
	}
	p := config.Profile{
		KubeContext:           d.Context,
		Service:               d.Server.Name,
		RemotePort:            d.Server.Port,
		Server:                (&url.URL{Scheme: scheme, Host: "127.0.0.1:" + strconv.Itoa(d.Server.Port)}).String(),
		Namespace:             d.Namespace,
		TokenEnv:              d.TokenEnv,
		TokenFile:             d.TokenFile,
		CAFile:                d.CAFile,
		InsecureSkipTLSVerify: d.Insecure,
	}
	if d.Server.Namespace != d.Namespace {
		p.ServiceNamespace = d.Server.Namespace
	}
	if d.ServerAuth {
		p.AuthMode = "server"
	}
	return p
}

// draftConfig validates a draft the way a saved profile is validated.
func draftConfig(d profiles.Draft) (config.Config, error) {
	data, err := yaml.Marshal(config.File{Profiles: map[string]config.Profile{probeName: draftProfile(d)}})
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(data, config.Options{Profile: probeName})
}
