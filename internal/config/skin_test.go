package config

import (
	"strings"
	"testing"
)

var knownSkins = []string{"auto", "default", "nord", "dracula"}

const yamlSkins = `
currentProfile: dev
skin: nord
profiles:
  dev:
    server: https://dev.example.test/argo
    namespace: workflows
    tokenEnv: MICKO_TOKEN
  prod:
    server: https://prod.example.test/argo
    namespace: prod-wf
    tokenEnv: MICKO_TOKEN
    skin: dracula
`

// The resolved skin follows flag, profile, file and default precedence, and must be known.
func TestSkinPrecedence(t *testing.T) {
	cases := []struct {
		name, data, profile, flag, want string
		known                           []string
	}{
		{"file", yamlSkins, "dev", "", "nord", knownSkins},
		{"profile", yamlSkins, "prod", "", "dracula", knownSkins},
		{"flag over profile", yamlSkins, "prod", "default", "default", knownSkins},
		{"flag over file", yamlSkins, "dev", "dracula", "dracula", knownSkins},
		{"default", yamlOneProfile, "", "", "default", knownSkins},
		{"case insensitive", yamlOneProfile, "", "Nord", "Nord", knownSkins},
		{"unspecified known names", yamlOneProfile, "", "anything", "anything", nil},
		{"unknown flag", yamlSkins, "dev", "neon", "", knownSkins},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Load([]byte(c.data), Options{Profile: c.profile, Skin: c.flag, Skins: c.known})
			if c.want == "" {
				if err == nil || !strings.Contains(err.Error(), `unknown skin "neon"`) {
					t.Fatalf("Load error = %v, want unknown neon skin", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Skin != c.want {
				t.Fatalf("skin = %q, want %q", got.Skin, c.want)
			}
		})
	}
}

// Startup reports unknown skins even on unselected profiles, while parse errors remain for the picker.
func TestValidateSkins(t *testing.T) {
	cases := []struct{ name, data, where string }{
		{"unknown file skin", "skin: solarised\nprofiles:\n  dev:\n    server: https://a.test\n", "skin"},
		{"unknown unselected profile", "profiles:\n  dev:\n    server: https://a.test\n  prod:\n    server: https://b.test\n    skin: solarised\n", `profile "prod"`},
		{"known skins", yamlSkins, ""},
		{"malformed file", "profiles: [not a map\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSkins([]byte(c.data), knownSkins)
			if c.where == "" {
				if err != nil {
					t.Fatalf("ValidateSkins = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("unknown skin accepted")
			}
			for _, want := range append([]string{`"solarised"`, c.where}, knownSkins...) {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %v, want %q", err, want)
				}
			}
		})
	}
}

// The picker's skin comes only from the top level; absent and malformed files name none.
func TestFileSkin(t *testing.T) {
	for _, c := range []struct{ name, data, want string }{
		{"top level", yamlSkins, "nord"},
		{"profile only", "profiles:\n  a:\n    skin: dracula\n", ""},
		{"malformed file", "profiles: [not a map\n", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := FileSkin([]byte(c.data)); got != c.want {
				t.Fatalf("FileSkin = %q, want %q", got, c.want)
			}
		})
	}
}
