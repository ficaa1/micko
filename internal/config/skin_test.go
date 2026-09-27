package config

import (
	"strings"
	"testing"
)

// knownSkins stands in for the UI's skin list, which this package does not
// import.
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

// The skin resolves like every other setting: the flag, then the profile,
// then the file's top level, then the default.
func TestSkinPrecedenceFlagOverProfileOverFile(t *testing.T) {
	cases := []struct {
		name, profile, flag, want string
	}{
		{name: "top level applies to a profile without its own", profile: "dev", want: "nord"},
		{name: "a profile's skin overrides the top level", profile: "prod", want: "dracula"},
		{name: "the flag overrides a profile's skin", profile: "prod", flag: "default", want: "default"},
		{name: "the flag overrides the top level", profile: "dev", flag: "dracula", want: "dracula"},
	}
	for _, c := range cases {
		cfg, err := Load([]byte(yamlSkins), Options{Profile: c.profile, Skin: c.flag, Skins: knownSkins})
		if err != nil {
			t.Fatalf("%s: Load: %v", c.name, err)
		}
		if cfg.Skin != c.want {
			t.Errorf("%s: skin = %q, want %q", c.name, cfg.Skin, c.want)
		}
	}
}

// A file that names no skin gets the default one, never an empty name.
func TestSkinDefaultsWhenNothingNamesOne(t *testing.T) {
	cfg, err := Load([]byte(yamlOneProfile), Options{Skins: knownSkins})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Skin != DefaultSkin {
		t.Errorf("skin = %q, want %q", cfg.Skin, DefaultSkin)
	}
}

// An unknown skin, at the top level or on any profile, is reported when the
// file is read, with where it was found and every valid name.
func TestUnknownSkinIsRejectedWithTheValidNames(t *testing.T) {
	cases := map[string]string{
		"top level": "skin: solarised\nprofiles:\n  dev:\n    server: https://a.test\n",
		"profile":   "profiles:\n  dev:\n    server: https://a.test\n  prod:\n    server: https://b.test\n    skin: solarised\n",
	}
	for name, y := range cases {
		err := ValidateSkins([]byte(y), knownSkins)
		if err == nil {
			t.Fatalf("%s: an unknown skin was accepted", name)
		}
		msg := err.Error()
		if !strings.Contains(msg, `"solarised"`) {
			t.Errorf("%s: error does not name the skin: %v", name, err)
		}
		if name == "profile" && !strings.Contains(msg, `profile "prod"`) {
			t.Errorf("%s: error does not name the profile: %v", name, err)
		}
		for _, k := range knownSkins {
			if !strings.Contains(msg, k) {
				t.Errorf("%s: error does not list %q: %v", name, k, err)
			}
		}
	}
	if err := ValidateSkins([]byte(yamlSkins), knownSkins); err != nil {
		t.Errorf("valid skins rejected: %v", err)
	}
}

// Load checks the skin it resolves, so a flag or a profile outside the known
// set cannot reach the session.
func TestLoadRejectsAnUnknownSkin(t *testing.T) {
	if _, err := Load([]byte(yamlSkins), Options{Profile: "dev", Skin: "neon", Skins: knownSkins}); err == nil {
		t.Fatal("Load accepted an unknown flag skin")
	}
}

// Names match without case, the way a reader types them.
func TestCheckSkinIgnoresCase(t *testing.T) {
	if err := CheckSkin("skin", "Nord", knownSkins); err != nil {
		t.Errorf("CheckSkin(Nord) = %v", err)
	}
	if err := CheckSkin("skin", "anything", nil); err != nil {
		t.Errorf("an empty known list must accept every name: %v", err)
	}
}

// A file that does not parse is the picker's to report; the skin check must
// not turn it into a startup failure first.
func TestValidateSkinsLeavesAParseErrorToThePicker(t *testing.T) {
	if err := ValidateSkins([]byte("profiles: [not a map\n"), knownSkins); err != nil {
		t.Errorf("ValidateSkins = %v, want nil for an unparsable file", err)
	}
	if got := FileSkin([]byte("profiles: [not a map\n")); got != "" {
		t.Errorf("FileSkin of an unparsable file = %q", got)
	}
}

// FileSkin is the top-level skin alone; a profile's skin is not it.
func TestFileSkinReadsOnlyTheTopLevel(t *testing.T) {
	if got := FileSkin([]byte(yamlSkins)); got != "nord" {
		t.Errorf("FileSkin = %q, want nord", got)
	}
	if got := FileSkin([]byte("profiles:\n  a:\n    skin: dracula\n")); got != "" {
		t.Errorf("FileSkin = %q, want empty", got)
	}
}
