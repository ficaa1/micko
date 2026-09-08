package core

import (
	"context"
	"fmt"
)

// WatchRequest starts a watch from an opaque server cursor. The caller owns
// relisting/reconnect policy; the watcher never invents or parses cursors.
type WatchRequest struct {
	Namespace       string
	LabelSelector   string
	ResourceVersion string
}

// WatchEvent is the normalized watch contract. Unknown Type values remain
// valid so newer Argo servers do not break the client.
type WatchEvent struct {
	Type            string
	Summary         Summary
	ResourceVersion string
}

const (
	WatchAdded    = "ADDED"
	WatchModified = "MODIFIED"
	WatchDeleted  = "DELETED"
	WatchBookmark = "BOOKMARK"
)

// Watcher streams events serially until cancellation, callback failure, EOF,
// or a typed watch error. It does not retry or relist.
type Watcher interface {
	Watch(context.Context, WatchRequest, func(WatchEvent) error) error
}

type WatchErrorKind string

const (
	WatchExpired     WatchErrorKind = "expired"
	WatchUnsupported WatchErrorKind = "unsupported"
	WatchProtocol    WatchErrorKind = "protocol"
	WatchEnded       WatchErrorKind = "ended"
)

// WatchError preserves the last accepted cursor and tells the coordinator
// whether a relist is required. LastResourceVersion is opaque.
type WatchError struct {
	Kind                WatchErrorKind
	Message             string
	LastResourceVersion string
	Err                 error
}

func NewWatchError(kind WatchErrorKind, message, lastResourceVersion string, cause error) *WatchError {
	return &WatchError{Kind: kind, Message: message, LastResourceVersion: lastResourceVersion, Err: cause}
}

func (e *WatchError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Message == "" {
		return string(e.Kind)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *WatchError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}
