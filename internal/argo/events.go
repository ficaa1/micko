package argo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/ficaa1/micko/internal/core"
)

// events.go streams Kubernetes events through the Argo server:
//
//	GET /api/v1/stream/events/{namespace}?listOptions.fieldSelector=...
//
// (WorkflowService_WatchEvents). The server opens a Kubernetes watch and
// sends each event as {"result": <io.k8s.api.core.v1.Event>} in JSON lines
// or SSE frames, with {"error": ...} in band. The watch type does not
// survive the gateway, so a result is the event itself; the {"type",
// "object"} shape a Kubernetes watch writes is accepted too. The framing,
// the size cap, keepalives and error classification are the workflow
// watch's (readWatchStream), so both streams end the same ways and the
// caller reconnects them by the same rules.

// wireEvent is the part of io.k8s.api.core.v1.Event the view reads. Times
// are strings so a null, an empty string or a MicroTime all decode.
type wireEvent struct {
	Metadata struct {
		UID               string `json:"uid"`
		Namespace         string `json:"namespace"`
		ResourceVersion   string `json:"resourceVersion"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	InvolvedObject struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"involvedObject"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
	Type           string `json:"type"`
	Count          int    `json:"count"`
	FirstTimestamp string `json:"firstTimestamp"`
	LastTimestamp  string `json:"lastTimestamp"`
	EventTime      string `json:"eventTime"`
	Series         *struct {
		Count            int    `json:"count"`
		LastObservedTime string `json:"lastObservedTime"`
	} `json:"series"`
	Source struct {
		Component string `json:"component"`
	} `json:"source"`
	ReportingComponent string `json:"reportingComponent"`
}

// WatchEvents implements core.EventWatcher. It never retries or
// reconnects. A server that has no event stream (404, 501) ends it with a
// WatchUnsupported error, so the caller can say so instead of retrying.
func (c *Client) WatchEvents(ctx context.Context, req core.EventWatchRequest, cb func(core.Event) error) error {
	path := "/api/v1/stream/events/" + url.PathEscape(req.Namespace)
	query := url.Values{}
	if req.FieldSelector != "" {
		query.Set("listOptions.fieldSelector", req.FieldSelector)
	}
	if req.ResourceVersion != "" {
		query.Set("listOptions.resourceVersion", req.ResourceVersion)
	}
	body, err := c.openWatch(ctx, path, query, req.ResourceVersion)
	if err != nil {
		var we *core.WatchError
		if ae := core.AsAPIError(err); ae != nil && errors.As(err, &we) &&
			(ae.Status == http.StatusNotFound || ae.Status == http.StatusNotImplemented) {
			we.Kind = core.WatchUnsupported
		}
		return err
	}
	defer drainAndClose(body)
	return readWatchStream(ctx, body, req.ResourceVersion, true, func(typ string, object json.RawMessage, lastRV string) (string, error) {
		ev, err := decodeEvent(object)
		if err != nil {
			return "", core.NewWatchError(core.WatchProtocol, "event object is invalid", lastRV, err)
		}
		if typ == core.WatchBookmark {
			return ev.ResourceVersion, nil
		}
		ev.Deleted = typ == core.WatchDeleted
		return ev.ResourceVersion, cb(ev)
	})
}

// decodeEvent reads one event. Count is at least 1; the series count wins
// when the server sent one. LastSeen is the series' last observation, the
// last timestamp, the event time or the creation time, the first of them
// that is set.
func decodeEvent(raw json.RawMessage) (core.Event, error) {
	var w wireEvent
	if err := json.Unmarshal(raw, &w); err != nil {
		return core.Event{}, err
	}
	ev := core.Event{
		UID:             w.Metadata.UID,
		ResourceVersion: w.Metadata.ResourceVersion,
		Namespace:       firstNonEmpty(w.InvolvedObject.Namespace, w.Metadata.Namespace),
		Type:            w.Type,
		Reason:          w.Reason,
		Message:         w.Message,
		ObjectKind:      w.InvolvedObject.Kind,
		ObjectName:      w.InvolvedObject.Name,
		Count:           w.Count,
		Source:          firstNonEmpty(w.Source.Component, w.ReportingComponent),
	}
	last := ""
	if w.Series != nil {
		if w.Series.Count > ev.Count {
			ev.Count = w.Series.Count
		}
		last = w.Series.LastObservedTime
	}
	if ev.Count < 1 {
		ev.Count = 1
	}
	ev.LastSeen = firstTime(last, w.LastTimestamp, w.EventTime, w.Metadata.CreationTimestamp)
	ev.FirstSeen = firstTime(w.FirstTimestamp, w.EventTime, w.Metadata.CreationTimestamp)
	if ev.FirstSeen.IsZero() || ev.FirstSeen.After(ev.LastSeen) {
		ev.FirstSeen = ev.LastSeen
	}
	return ev, nil
}

// firstTime parses the first of ts that is a valid RFC 3339 time.
func firstTime(ts ...string) time.Time {
	for _, s := range ts {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// compile-time proof the client streams events.
var _ core.EventWatcher = (*Client)(nil)
