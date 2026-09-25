package core

import (
	"context"
	"time"
)

// EventWatchRequest opens a stream of the Kubernetes events in one
// namespace. FieldSelector narrows it on the server in Kubernetes field
// selector syntax ("involvedObject.kind=Pod,reason=BackOff"); the API
// server accepts only equality terms joined by commas for events, so one
// selector cannot name a workflow and its pods together. ResourceVersion
// resumes a stream from the last event a previous one delivered; empty
// starts with the events that exist now.
type EventWatchRequest struct {
	Namespace       string
	FieldSelector   string
	ResourceVersion string
}

// Event is one Kubernetes event about an object.
type Event struct {
	UID             string
	ResourceVersion string
	Namespace       string
	// Type is "Normal" or "Warning" as the server sent it. An unknown type
	// stays displayable.
	Type    string
	Reason  string
	Message string
	// ObjectKind and ObjectName name the involved object.
	ObjectKind string
	ObjectName string
	// Count is how many times the event happened, at least 1.
	Count int
	// FirstSeen and LastSeen bound when it happened. LastSeen falls back to
	// the event's own time and then its creation time, so it is set
	// whenever the server gave any time at all.
	FirstSeen time.Time
	LastSeen  time.Time
	// Source is the component that reported it, such as kubelet or
	// workflow-controller.
	Source string
	// Deleted marks an event the stream reported as deleted.
	Deleted bool
}

// EventWatcher streams Kubernetes events serially until cancellation,
// callback failure, end of stream, or a typed *WatchError. Like Watcher it
// never retries or reconnects; the caller owns that policy.
type EventWatcher interface {
	WatchEvents(context.Context, EventWatchRequest, func(Event) error) error
}
