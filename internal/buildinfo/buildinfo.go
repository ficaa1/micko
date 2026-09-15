// Package buildinfo is the single source of the application's version and
// commit. Version is a constant so a plain `go build` stays correct; Commit
// is injected by -X github.com/ficaa1/argo-tui/internal/buildinfo.Commit=<sha>.
package buildinfo

import "runtime/debug"

const Version = "0.3.1"

// Commit is the short revision this binary was built from. The Makefile and
// the release workflow inject it. A build that injects nothing — `go install`,
// or a plain `go build` — falls back to the revision Go itself recorded, so
// the binary can still say which commit it is.
var Commit = vcsRevision()

// UserAgent is the User-Agent sent to the Argo server. The commit is left
// out to keep the header low-cardinality in server logs.
func UserAgent() string { return "argo-tui/" + Version }

// vcsRevision reads the revision Go stamps into a binary built inside a git
// work tree. A build from a source archive has none, and a dirty tree makes
// the revision misleading, so both report "unknown" rather than a commit the
// binary does not actually match.
func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, modified := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if rev == "" || modified {
		return "unknown"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	return rev
}
