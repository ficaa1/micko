package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A missing config file is not a failure. The picker opens on the path that
// does not exist yet, so the reader is told where to write one.
func TestAMissingConfigFileStillNamesThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	c, err := NewConnector(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	list := c.ProfileList()
	if list.ConfigPath != path {
		t.Errorf("ConfigPath = %q, want %q", list.ConfigPath, path)
	}
	if len(list.Items) != 0 {
		t.Errorf("Items = %+v, want none", list.Items)
	}
	if c.HasProfiles() {
		t.Error("HasProfiles() is true with no file")
	}
}

// An unreadable file must say so. An empty picker would otherwise look like a
// file the reader never wrote.
func TestABrokenConfigFileIsReportedNotHidden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("profiles: [this is not a map\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewConnector(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if c.ProfileList().Err == "" {
		t.Error("a config file that does not parse was reported as an empty one")
	}
}

// The profiles the picker offers come from the file, with the server and
// namespace that tell two clusters apart.
func TestTheConnectorListsTheConfiguredProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "currentProfile: prod\nprofiles:\n" +
		"  dev:\n    server: https://dev.example.com\n    namespace: workflows\n    tokenEnv: T\n" +
		"  prod:\n    server: https://prod.example.com\n    namespace: argo\n    tokenEnv: T\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewConnector(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	list := c.ProfileList()
	if list.Current != "prod" {
		t.Errorf("Current = %q, want prod", list.Current)
	}
	if len(list.Items) != 2 || list.Items[0].Name != "dev" {
		t.Fatalf("Items = %+v, want dev then prod", list.Items)
	}
	if list.Items[0].Server != "https://dev.example.com" {
		t.Errorf("dev server = %q", list.Items[0].Server)
	}
}

// Connecting to a profile the file does not name fails by name. Nothing is
// started, so the picker can report it and stay open.
func TestConnectRejectsAProfileThatIsNotConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("profiles:\n  dev:\n    server: https://dev.example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewConnector(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Connect(context.Background(), "missing")
	if err == nil {
		t.Fatal("Connect accepted a profile that is not in the file")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %v, want it to name the profile", err)
	}
}

// A profile with no port-forward target connects without starting one, and the
// connection then has no transport lifecycle to report.
func TestAProfileWithNoForwardConnectsDirectly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "profiles:\n  dev:\n    server: https://argo.example.com\n    namespace: argo\n    tokenEnv: MICKO_TEST_TOKEN\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewConnector(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := c.Connect(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if conn.Profile != "dev" || conn.Namespace != "argo" {
		t.Errorf("connection = %+v, want the profile's own values", conn)
	}
	if conn.States != nil {
		t.Error("a connection with no port-forward reported a transport lifecycle")
	}
	if conn.Close != nil {
		t.Error("a connection with no port-forward has nothing to close")
	}
}

// Close releases whatever connection the connector last handed out, so an exit
// path the model did not take still stops the forward.
func TestCloseIsSafeWithNoConnection(t *testing.T) {
	c, err := NewConnector(Options{ConfigPath: filepath.Join(t.TempDir(), "config.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	c.Close()
}

// The forward moves the transport to a loopback port the program owns. It
// must not silently downgrade a configured https endpoint to plain http, and
// it must not drop a configured base path: both would send a bearer token
// somewhere the profile never named.
func TestForwardEndpointKeepsTheSchemeAndPath(t *testing.T) {
	cases := []struct {
		name                        string
		configured, announced, want string
	}{
		{
			name:       "https survives the move to loopback",
			configured: "https://argo.example.com",
			announced:  "http://127.0.0.1:51234",
			want:       "https://127.0.0.1:51234",
		},
		{
			name:       "a base path is kept",
			configured: "https://argo.example.com/argo/",
			announced:  "http://127.0.0.1:51234",
			want:       "https://127.0.0.1:51234/argo",
		},
		{
			name:       "no announced port means no endpoint",
			configured: "https://argo.example.com",
			announced:  "",
			want:       "",
		},
		{
			name:       "an unparsable announcement is refused",
			configured: "https://argo.example.com",
			announced:  "not a url",
			want:       "",
		},
	}
	for _, c := range cases {
		if got := forwardEndpoint(c.configured, c.announced); got != c.want {
			t.Errorf("%s: forwardEndpoint(%q, %q) = %q, want %q", c.name, c.configured, c.announced, got, c.want)
		}
	}
}
