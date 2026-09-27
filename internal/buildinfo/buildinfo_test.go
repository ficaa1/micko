package buildinfo

import "testing"

// The version is the number the release tag is checked against, so it must
// always be a readable major.minor.patch.
func TestVersionIsAReleaseNumber(t *testing.T) {
	if Version == "" {
		t.Fatal("Version is empty")
	}
	dots := 0
	for _, r := range Version {
		if r == '.' {
			dots++
		}
	}
	if dots < 2 {
		t.Errorf("Version = %q, want major.minor.patch", Version)
	}
}

// A binary must never claim a commit it was not built from. Anything the build
// did not record, and anything built from a dirty tree, says "unknown".
func TestCommitIsNeverEmpty(t *testing.T) {
	if Commit == "" {
		t.Error("Commit is empty; it must say unknown instead")
	}
}

// The User-Agent names the program and its version and nothing else, so it
// stays low-cardinality in a server's logs.
func TestUserAgentCarriesOnlyTheVersion(t *testing.T) {
	if got, want := UserAgent(), "micko/"+Version; got != want {
		t.Errorf("UserAgent() = %q, want %q", got, want)
	}
}
