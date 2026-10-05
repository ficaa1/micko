package app

import (
	"context"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/diagnostics"
)

// The root holds a Connector and knows nothing about kubectl, TLS or ports:
// building a profile's transport is the entrypoint's work.

// Connection is one live connection to an Argo server.
type Connection struct {
	Reader core.Reader
	// The profile's display values, replaced wholesale on a switch.
	Profile     string
	Server      string
	Namespace   string
	WebURL      string
	PipeCommand string
	// Skin is the profile's resolved palette; empty keeps the current one.
	Skin string
	// Redact starts every workflow with its values hidden.
	Redact bool
	// Namespaces are the profile's namespaces, offered by the n picker.
	Namespaces []string
	Interval   time.Duration
	// States carries the port-forward's lifecycle changes and is closed with the
	// connection. Nil without a port-forward.
	States <-chan ConnectionStateMsg
	// Close releases the transport. Nil when there is nothing to release.
	Close func()
	// Diagnostics receives debug spans; nil disables them.
	Diagnostics *diagnostics.Sink
}

// Connector opens a connection by profile name. Connect can take seconds to
// start a port-forward, so the root calls it from a command.
type Connector interface {
	Connect(ctx context.Context, profile string) (*Connection, error)
}
