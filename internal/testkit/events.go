package testkit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ficaa1/argo-tui/internal/core"
)

// events.go is the fake backend's Kubernetes event stream, and the demo's
// synthetic events.

// WatchEvents implements core.EventWatcher. It sends the stored events that
// match the request's namespace and field selector, oldest first, then every
// event PublishEvent sends that matches, until the context ends. The field
// selector takes the equality terms the API server takes for events
// (involvedObject.kind, involvedObject.name, involvedObject.namespace, type,
// reason); any other term fails the stream, as the server would.
func (f *FakeReader) WatchEvents(ctx context.Context, req core.EventWatchRequest, cb func(core.Event) error) error {
	f.mu.Lock()
	f.EventStarts++
	f.EventRequests = append(f.EventRequests, req)
	hook, err := f.EventHook, f.EventsErr
	stored := append([]core.Event(nil), f.Events...)
	feed := make(chan core.Event, 64)
	if f.eventSubs == nil {
		f.eventSubs = map[chan core.Event]bool{}
	}
	f.eventSubs[feed] = true
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.eventSubs, feed)
		f.mu.Unlock()
	}()

	if hook != nil {
		if herr := hook(req); herr != nil {
			return herr
		}
	}
	if err != nil {
		return err
	}
	match, err := eventMatcher(req)
	if err != nil {
		return err
	}
	sort.SliceStable(stored, func(i, j int) bool { return stored[i].LastSeen.Before(stored[j].LastSeen) })
	for _, e := range stored {
		if !match(e) {
			continue
		}
		if err := cb(e); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			f.mu.Lock()
			f.EventCancels++
			f.mu.Unlock()
			return ctx.Err()
		case e := <-feed:
			if !match(e) {
				continue
			}
			if err := cb(e); err != nil {
				return err
			}
		}
	}
}

// eventMatcher turns a request into a filter over events.
func eventMatcher(req core.EventWatchRequest) (func(core.Event) bool, error) {
	type term struct{ key, value string }
	var terms []term
	if req.FieldSelector != "" {
		for _, t := range strings.Split(req.FieldSelector, ",") {
			k, v, ok := strings.Cut(t, "=")
			if !ok {
				return nil, core.NewWatchError(core.WatchProtocol, "unsupported field selector term "+t, "", nil)
			}
			switch k {
			case "involvedObject.kind", "involvedObject.name", "involvedObject.namespace", "type", "reason":
			default:
				return nil, core.NewWatchError(core.WatchProtocol, "field label not supported: "+k, "", nil)
			}
			terms = append(terms, term{k, v})
		}
	}
	return func(e core.Event) bool {
		if req.Namespace != "" && e.Namespace != req.Namespace {
			return false
		}
		for _, t := range terms {
			var got string
			switch t.key {
			case "involvedObject.kind":
				got = e.ObjectKind
			case "involvedObject.name":
				got = e.ObjectName
			case "involvedObject.namespace":
				got = e.Namespace
			case "type":
				got = e.Type
			case "reason":
				got = e.Reason
			}
			if got != t.value {
				return false
			}
		}
		return true
	}, nil
}

// PublishEvent sends e to every event stream open now; each passes it on
// when it matches the stream's request. A stream whose queue is full misses
// it, as a slow client would.
func (f *FakeReader) PublishEvent(e core.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.eventSubs {
		select {
		case ch <- e:
		default:
		}
	}
}

// EventStartCount and EventCancelCount report how many event streams were
// opened and how many ended by cancellation (thread-safe, for tests).
func (f *FakeReader) EventStartCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.EventStarts
}

func (f *FakeReader) EventCancelCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.EventCancels
}

// EventRequestLog is every event stream request so far, in order.
func (f *FakeReader) EventRequestLog() []core.EventWatchRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]core.EventWatchRequest(nil), f.EventRequests...)
}

var _ core.EventWatcher = (*FakeReader)(nil)

// demoEvents fabricates the Kubernetes events a cluster would record for a
// demo workflow: the controller's events on the workflow, and the
// scheduler's and kubelet's on each pod that started. A failing retry
// attempt backs off and an out-of-memory pod is OOM-killed, so the Events
// section has warnings to show beside the normal traffic.
func demoEvents(wf core.Workflow) []core.Event {
	var out []core.Event
	seq := 0
	add := func(kind, name, typ, reason, message, source string, first time.Time, last time.Time, count int) {
		seq++
		out = append(out, core.Event{
			UID:             fmt.Sprintf("demo-ev-%d", fnv32(wf.Summary.Ref.Name+"/"+name+"/"+reason+"/"+first.String())),
			ResourceVersion: fmt.Sprint(1000 + seq),
			Namespace:       wf.Summary.Ref.Namespace,
			Type:            typ, Reason: reason, Message: message,
			ObjectKind: kind, ObjectName: name, Count: count,
			FirstSeen: first, LastSeen: last, Source: source,
		})
	}
	name := wf.Summary.Ref.Name
	s := wf.Summary
	if s.StartedAt != nil && len(wf.Nodes) > 0 {
		add("Workflow", name, "Normal", "WorkflowRunning", "Workflow Running", "workflow-controller", *s.StartedAt, *s.StartedAt, 1)
	}
	ids := make([]string, 0, len(wf.Nodes))
	for id := range wf.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := wf.Nodes[id]
		if n.Type != "Pod" || n.PodName == "" || n.StartedAt == nil {
			continue
		}
		at := *n.StartedAt
		image := "registry.example/demo/" + n.TemplateName + ":1.4.2"
		add("Pod", n.PodName, "Normal", "Scheduled", "Successfully assigned "+wf.Summary.Ref.Namespace+"/"+n.PodName+" to "+n.HostNodeName,
			"default-scheduler", at, at, 1)
		add("Pod", n.PodName, "Normal", "Pulled", "Container image \""+image+"\" already present on machine", "kubelet",
			at.Add(time.Second), at.Add(time.Second), 1)
		add("Pod", n.PodName, "Normal", "Created", "Created container main", "kubelet", at.Add(time.Second), at.Add(time.Second), 1)
		add("Pod", n.PodName, "Normal", "Started", "Started container main", "kubelet", at.Add(2*time.Second), at.Add(2*time.Second), 1)
		if n.FinishedAt == nil {
			continue
		}
		end := *n.FinishedAt
		switch {
		case strings.Contains(n.Message, "OOMKilled"):
			add("Pod", n.PodName, "Warning", "OOMKilling",
				"Memory cgroup out of memory: Killed process 4127 (python) total-vm:2621440kB, anon-rss:2097152kB", "kernel-monitor",
				end, end, 1)
		case n.Retried && (n.Phase == "Failed" || n.Phase == "Error"):
			add("Pod", n.PodName, "Warning", "BackOff", "Back-off restarting failed container main in pod "+n.PodName, "kubelet",
				end.Add(-20*time.Second), end, 3)
		}
		if n.Phase == "Failed" || n.Phase == "Error" {
			add("Workflow", name, "Warning", "WorkflowNodeFailed", "Failed node "+n.Name+": "+n.Message, "workflow-controller", end, end, 1)
		}
	}
	if s.FinishedAt != nil {
		switch s.Phase {
		case "Failed", "Error":
			add("Workflow", name, "Warning", "WorkflowFailed", s.Message, "workflow-controller", *s.FinishedAt, *s.FinishedAt, 1)
		case "Succeeded":
			add("Workflow", name, "Normal", "WorkflowSucceeded", "Workflow completed", "workflow-controller", *s.FinishedAt, *s.FinishedAt, 1)
		}
	}
	return out
}
