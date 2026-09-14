// Package buildinfo is the single source of the application's version and
// commit. Version is a constant so a plain `go build` stays correct; Commit
// is injected by -X argo-tui/internal/buildinfo.Commit=<sha>.
package buildinfo

const Version = "0.2.1"

var Commit = "unknown"

// UserAgent is the User-Agent sent to the Argo server. The commit is left
// out to keep the header low-cardinality in server logs.
func UserAgent() string { return "argo-tui/" + Version }
