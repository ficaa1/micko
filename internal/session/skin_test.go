package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var testSkins = []string{"auto", "default", "nord", "dracula"}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
func TestTheStartingSkinPrefersTheFlag(t *testing.T) {
	path := writeConfig(t, "skin: nord\nprofiles:\n  dev:\n    server: https://a.test\n")
	c, err := NewConnector(Options{ConfigPath: path, Skins: testSkins})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Skin(); got != "nord" {
		t.Errorf("file skin: Skin() = %q, want nord", got)
	}
	c, err = NewConnector(Options{ConfigPath: path, Skin: "dracula", Skins: testSkins})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Skin(); got != "dracula" {
		t.Errorf("flag skin: Skin() = %q, want dracula", got)
	}
	c, err = NewConnector(Options{ConfigPath: filepath.Join(t.TempDir(), "none.yaml"), Skins: testSkins})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Skin(); got != "default" {
		t.Errorf("no file: Skin() = %q, want default", got)
	}
}

// A connection carries its profile's skin, so a switch redraws the session
// in the palette that profile names.
func TestAConnectionCarriesItsProfilesSkin(t *testing.T) {
	path := writeConfig(t, "skin: nord\nprofiles:\n"+
		"  dev:\n    server: https://argo.example.com\n    namespace: argo\n    tokenEnv: T\n"+
		"  prod:\n    server: https://argo.example.com\n    namespace: argo\n    tokenEnv: T\n    skin: dracula\n")
	c, err := NewConnector(Options{ConfigPath: path, Skins: testSkins})
	if err != nil {
		t.Fatal(err)
	}
	for profile, want := range map[string]string{"dev": "nord", "prod": "dracula"} {
		conn, err := c.Connect(context.Background(), profile)
		if err != nil {
			t.Fatal(err)
		}
		if conn.Skin != want {
			t.Errorf("%s: connection skin = %q, want %q", profile, conn.Skin, want)
		}
	}
}
