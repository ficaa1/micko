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
)

type Event struct {
	Time          time.Time `json:"time"`
	Stage         Stage     `json:"stage"`
	State         string    `json:"state,omitempty"`
	Retry         int       `json:"retry,omitempty"`
	Uncertain     bool      `json:"uncertain,omitempty"`
	Status        int       `json:"status,omitempty"`
	ErrorCategory string    `json:"error_category,omitempty"`
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
	e := Event{Time: time.Now().UTC(), Stage: stage, State: state, Retry: retry, Uncertain: uncertain, Status: status, ErrorCategory: category}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = json.NewEncoder(s.w).Encode(e)
}

func validStage(s Stage) bool {
	switch s {
	case StageConnect, StageForward, StageRequest, StageAction:
		return true
	}
	return false
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
