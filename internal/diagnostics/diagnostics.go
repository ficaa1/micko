// Package diagnostics provides optional, deliberately small operator diagnostics.
// Callers supply only allowlisted lifecycle facts; no arbitrary payload is accepted.
package diagnostics

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"
)

type Stage string

const (
	StageConnect Stage = "connect"
	StageForward Stage = "forward"
	StageRequest Stage = "request"
	StageAction  Stage = "action"
	StageSpan    Stage = "span"
)

type Event struct {
	Time          time.Time `json:"time"`
	Stage         Stage     `json:"stage"`
	State         string    `json:"state,omitempty"`
	Retry         int       `json:"retry,omitempty"`
	Uncertain     bool      `json:"uncertain,omitempty"`
	Status        int       `json:"status,omitempty"`
	ErrorCategory string    `json:"error_category,omitempty"`

	// Span is set on span timings only, with TotalMS.
	Span string `json:"span,omitempty"`

	// The fields below are set on request timings only.
	Endpoint  string  `json:"endpoint,omitempty"`
	Conn      string  `json:"conn,omitempty"`
	ConnectMS float64 `json:"connect_ms,omitempty"`
	TLSMS     float64 `json:"tls_ms,omitempty"`
	TTFBMS    float64 `json:"ttfb_ms,omitempty"`
	TotalMS   float64 `json:"total_ms,omitempty"`
	Bytes     int64   `json:"bytes,omitempty"`
}

// Request contains HTTP timings with a fixed endpoint name instead of a path.
type Request struct {
	Endpoint string
	Failed   bool
	Status   int
	Reused   bool
	// Total includes body reads and spans the lifetime of a streaming response.
	Connect, TLS, TTFB, Total time.Duration
	Bytes                     int64
}

// Sink writes newline-delimited JSON diagnostics. A nil sink disables output.
type Sink struct {
	mu sync.Mutex
	w  io.Writer
}

func New(w io.Writer) *Sink { return &Sink{w: w} }

// Emit records one safe lifecycle event. Unknown stages/states/categories are
// omitted rather than allowing caller text into the diagnostic stream.
func (s *Sink) Emit(stage Stage, state string, retry int, uncertain bool, status int, category string) {
	if s == nil || s.w == nil {
		return
	}
	if !validStage(stage) || retry < 0 || status < 0 || !validToken(state) || !validToken(category) {
		return
	}
	s.write(Event{Time: time.Now().UTC(), Stage: stage, State: state, Retry: retry, Uncertain: uncertain, Status: status, ErrorCategory: category})
}

// EmitRequest writes a timing unless its endpoint or counts are invalid.
func (s *Sink) EmitRequest(r Request) {
	if s == nil || s.w == nil {
		return
	}
	if r.Endpoint == "" || !validToken(r.Endpoint) || r.Status < 0 || r.Bytes < 0 {
		return
	}
	e := Event{
		Time: time.Now().UTC(), Stage: StageRequest, State: "ok", Status: r.Status,
		Endpoint: r.Endpoint, Conn: "new",
		ConnectMS: millis(r.Connect), TLSMS: millis(r.TLS), TTFBMS: millis(r.TTFB), TotalMS: millis(r.Total),
		Bytes: r.Bytes,
	}
	if r.Failed {
		e.State = "failed"
	}
	if r.Reused {
		e.Conn = "reused"
	}
	s.write(e)
}

// EmitSpan writes a wait duration unless its name or duration is invalid.
func (s *Sink) EmitSpan(name string, d time.Duration, failed bool) {
	if s == nil || s.w == nil {
		return
	}
	if !validSpan(name) || d < 0 {
		return
	}
	e := Event{Time: time.Now().UTC(), Stage: StageSpan, State: "ok", Span: name, TotalMS: millis(d)}
	if failed {
		e.State = "failed"
	}
	s.write(e)
}

// millis is d in milliseconds, rounded to a tenth.
func millis(d time.Duration) float64 {
	return float64(d.Round(100*time.Microsecond)) / float64(time.Millisecond)
}

func (s *Sink) write(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = json.NewEncoder(s.w).Encode(e)
}

func validStage(s Stage) bool {
	switch s {
	case StageConnect, StageForward, StageRequest, StageAction, StageSpan:
		return true
	}
	return false
}

// validSpan reports whether name is lowercase words joined by underscores.
func validSpan(name string) bool {
	return name != "" && strings.Trim(name, "_") == name && validToken(strings.ReplaceAll(name, "_", ""))
}

func validToken(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return !strings.Contains(s, "token") && !strings.Contains(s, "secret")
}
