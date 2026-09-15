package app

import (
	"context"
	"time"

	"argo-tui/internal/core"
)

// connection.go is the boundary between the root model and the transport.
//
// The root switches profiles, and a profile carries a server, a credential
// source and often a port-forward target. Building those is the entrypoint's
// work, not the model's, so the model holds a Connector and knows nothing
// about kubectl, TLS or ports.

// Connection is one live connection to one Argo server: the reader every
// request goes through, the profile values the panes display, and the handle
// that releases the transport again.
type Connection struct {
	Reader core.Reader
	// Profile, Server, Namespace and the two optional addresses below are
	// display and behaviour values the root copies into its own state. They
	// belong to this profile and are replaced wholesale on a switch.
	Profile     string
	Server      string
	Namespace   string
	WebURL      string
	PipeCommand string
	// Namespaces are the namespaces the profile names, offered by the `n`
	// picker alongside whatever the server reports.
	Namespaces []string
	Interval   time.Duration
	// States carries transport lifecycle changes for this connection. It is
	// closed when the connection is closed, and nil when the profile needs no
	// port-forward and so has no lifecycle to report.
	States <-chan ConnectionStateMsg
	// Close releases the transport. Nil when there is nothing to release.
	Close func()
}

// Connector opens a connection by profile name.
//
// The root holds one and calls it for every profile switch. Connect blocks:
// starting a port-forward and waiting for it to bind a port takes seconds, so
// the root always calls it from a command and never from the update loop.
type Connector interface {
	Connect(ctx context.Context, profile string) (*Connection, error)
}
