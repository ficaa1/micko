package buildinfo

import (
	"regexp"
	"runtime/debug"
	"testing"
)

// The version is the number the release tag is checked against.
func TestVersionIsAReleaseNumber(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Errorf("Version = %q, want major.minor.patch", Version)
	}
}

// A binary never claims a commit it was not built from: a build with no
// recorded revision, or from a dirty tree, says "unknown".
func TestRevision(t *testing.T) {
	rev := debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef"}
	cases := []struct {
		name     string
		settings []debug.BuildSetting
		want     string
	}{
		{"clean tree", []debug.BuildSetting{rev, {Key: "vcs.modified", Value: "false"}}, "0123456"},
		{"dirty tree", []debug.BuildSetting{rev, {Key: "vcs.modified", Value: "true"}}, "unknown"},
		{"no revision", []debug.BuildSetting{{Key: "vcs.modified", Value: "false"}}, "unknown"},
		{"short revision", []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}, "abc"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := revision(c.settings); got != c.want {
				t.Errorf("revision = %q, want %q", got, c.want)
			}
		})
	}
}

// The User-Agent names the program and its version and nothing else, so it
// stays low-cardinality in a server's logs.
func TestUserAgentCarriesOnlyTheVersion(t *testing.T) {
	if got, want := UserAgent(), "micko/"+Version; got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
}
