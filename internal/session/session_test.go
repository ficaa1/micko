package session

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/profiles"
)

var testSkins = []string{"auto", "default", "nord", "dracula"}

// writeConfig returns the path of a config file holding body, or of a file
// that does not exist when body is empty.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if body == "" {
		return path
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The picker lists the file's profiles and names the file, and a file that
// does not parse is reported rather than shown as an empty one.
func TestProfileList(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantItems   []profiles.Item
		wantCurrent string
		wantErr     bool
	}{
		{"missing file", "", nil, "", false},
		{"broken file", "profiles: [this is not a map\n", nil, "", true},
		{"two profiles", "currentProfile: prod\nprofiles:\n" +
			"  dev:\n    server: https://dev.example.com\n    namespace: workflows\n    tokenEnv: T\n" +
			"  prod:\n    server: https://prod.example.com\n    namespace: argo\n    tokenEnv: T\n",
			[]profiles.Item{
				{Name: "dev", Server: "https://dev.example.com", Namespace: "workflows"},
				{Name: "prod", Server: "https://prod.example.com", Namespace: "argo"},
			}, "prod", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeConfig(t, c.body)
			conn, err := NewConnector(Options{ConfigPath: path})
			if err != nil {
				t.Fatal(err)
			}
			list := conn.ProfileList()
			if list.ConfigPath != path {
				t.Errorf("ConfigPath = %q, want %q", list.ConfigPath, path)
			}
			if !reflect.DeepEqual(list.Items, c.wantItems) {
				t.Errorf("Items = %+v, want %+v", list.Items, c.wantItems)
			}
			if list.Current != c.wantCurrent {
				t.Errorf("Current = %q, want %q", list.Current, c.wantCurrent)
			}
			if (list.Err != "") != c.wantErr {
				t.Errorf("Err = %q, want an error: %v", list.Err, c.wantErr)
			}
			if conn.HasProfiles() != (len(c.wantItems) > 0) {
				t.Errorf("HasProfiles() = %v with %d profiles", conn.HasProfiles(), len(c.wantItems))
			}
		})
	}
}

// A misspelled skin anywhere in the file stops the program at startup, even
// on a profile nobody has chosen yet.
func TestAnUnknownSkinIsAStartupError(t *testing.T) {
	path := writeConfig(t, "profiles:\n  dev:\n    server: https://a.test\n  prod:\n    server: https://b.test\n    skin: neon\n")
	_, err := NewConnector(Options{ConfigPath: path, Skins: testSkins})
	if err == nil {
		t.Fatal("a config file with an unknown skin was accepted")
	}
	if !strings.Contains(err.Error(), "neon") || !strings.Contains(err.Error(), "nord") {
		t.Errorf("error = %v, want the bad name and the valid ones", err)
	}
}

// Before a profile is chosen the flag wins, then the file's top level, then
// the default.
func TestStartingSkin(t *testing.T) {
	cases := []struct {
		name string
		body string
		flag string
		want string
	}{
		{"file", "skin: nord\nprofiles:\n  dev:\n    server: https://a.test\n", "", "nord"},
		{"flag over file", "skin: nord\nprofiles:\n  dev:\n    server: https://a.test\n", "dracula", "dracula"},
		{"no file", "", "", "default"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn, err := NewConnector(Options{ConfigPath: writeConfig(t, c.body), Skin: c.flag, Skins: testSkins})
			if err != nil {
				t.Fatal(err)
			}
			if got := conn.Skin(); got != c.want {
				t.Errorf("Skin() = %q, want %q", got, c.want)
			}
		})
	}
}

// A profile with no port-forward connects directly with its own namespace
// and skin, and a profile the file does not name fails by name.
func TestConnect(t *testing.T) {
	path := writeConfig(t, "skin: nord\nprofiles:\n"+
		"  dev:\n    server: https://argo.example.com\n    namespace: argo\n    tokenEnv: T\n"+
		"  prod:\n    server: https://argo.example.com\n    namespace: prod\n    tokenEnv: T\n    skin: dracula\n")
	cases := []struct {
		profile       string
		wantNamespace string
		wantSkin      string
		wantErr       string
	}{
		{"dev", "argo", "nord", ""},
		{"prod", "prod", "dracula", ""},
		{"missing", "", "", `"missing"`},
	}
	for _, c := range cases {
		t.Run(c.profile, func(t *testing.T) {
			connector, err := NewConnector(Options{ConfigPath: path, Skins: testSkins})
			if err != nil {
				t.Fatal(err)
			}
			conn, err := connector.Connect(context.Background(), c.profile)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("Connect error = %v, want one naming %s", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if conn.Profile != c.profile || conn.Namespace != c.wantNamespace || conn.Skin != c.wantSkin {
				t.Errorf("connection = profile %q namespace %q skin %q, want %q %q %q",
					conn.Profile, conn.Namespace, conn.Skin, c.profile, c.wantNamespace, c.wantSkin)
			}
			if conn.States != nil || conn.Close != nil {
				t.Error("a connection with no port-forward reported a transport lifecycle")
			}
		})
	}
}

// Requests write timings to the diagnostics writer with --debug only.
func TestRequestTimingsFollowDebug(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"metadata":{},"items":[]}`))
	}))
	t.Cleanup(srv.Close)
	path := writeConfig(t, "profiles:\n  dev:\n    server: "+srv.URL+"\n    namespace: argo\n    tokenEnv: T\n")
	for _, debug := range []bool{false, true} {
		var out bytes.Buffer
		connector, err := NewConnector(Options{ConfigPath: path, Skins: testSkins, Debug: debug, Diagnostics: &out})
		if err != nil {
			t.Fatal(err)
		}
		conn, err := connector.Connect(context.Background(), "dev")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Reader.List(context.Background(), core.Query{Namespace: "argo"}); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(out.String(), `"endpoint":"list"`); got != debug {
			t.Errorf("debug %v: diagnostics = %q, want a list timing %v", debug, out.String(), debug)
		}
	}
}

// The entrypoint always defers Close, so it is safe with nothing connected.
func TestCloseIsSafeWithNoConnection(t *testing.T) {
	c, err := NewConnector(Options{ConfigPath: writeConfig(t, "")})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	c.Close()
}

// The forward moves the transport to a loopback port the program owns. It
// must keep the configured scheme and base path, so a bearer token never goes
// somewhere the profile did not name.
func TestForwardEndpointKeepsTheSchemeAndPath(t *testing.T) {
	cases := []struct {
		name                        string
		configured, announced, want string
	}{
		{"https survives the move to loopback", "https://argo.example.com", "http://127.0.0.1:51234", "https://127.0.0.1:51234"},
		{"a base path is kept", "https://argo.example.com/argo/", "http://127.0.0.1:51234", "https://127.0.0.1:51234/argo"},
		{"no announced port means no endpoint", "https://argo.example.com", "", ""},
		{"an unparsable announcement is refused", "https://argo.example.com", "not a url", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := forwardEndpoint(c.configured, c.announced); got != c.want {
				t.Errorf("forwardEndpoint(%q, %q) = %q, want %q", c.configured, c.announced, got, c.want)
			}
		})
	}
}
