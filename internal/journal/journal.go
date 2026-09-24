// Package journal appends one JSON line per workflow write attempt to a
// local file, so an operator can answer "what did I change, where, and did
// it land" after the screen that reported it is gone.
//
// The journal is a record, never a gate. A failed append is returned to the
// caller to report; it must not block, delay or alter the action it
// describes, because the action has already happened (or been refused) by
// the time it is written.
package journal

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is one line of the journal. The field names are the file format:
// renaming one breaks every reader of an existing journal.
type Entry struct {
	Time      time.Time `json:"time"`
	Profile   string    `json:"profile"`
	Server    string    `json:"server"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	UID       string    `json:"uid"`
	Verb      string    `json:"verb"`
	// Outcome is confirmed, accepted, refused or unknown, the same words
	// the outcome pane shows.
	Outcome string `json:"outcome"`
	// Error is the error text that came with the outcome, empty when none.
	Error string `json:"error,omitempty"`
}

// fileMode keeps the journal private to its owner. It names workflows,
// namespaces and servers, which are nobody else's business on a shared host.
const fileMode = 0o600

// dirMode is the mode of a state directory the journal creates.
const dirMode = 0o700

// Journal appends entries to one file. The zero value and a nil *Journal
// are usable and record nothing, which is how a session with the journal
// turned off, or the demo, holds one.
type Journal struct {
	mu   sync.Mutex
	path string
	err  error
}

// New returns a journal writing to path. The file and its directory are
// created on the first append, not here: a session that never writes
// leaves nothing behind.
func New(path string) *Journal { return &Journal{path: path} }

// Open returns a journal at the default path. When no path can be resolved
// the journal still exists and reports that on every append, so the
// failure reaches the footer like any other.
func Open() *Journal {
	path, err := DefaultPath()
	return &Journal{path: path, err: err}
}

// Path is the file the journal appends to.
func (j *Journal) Path() string {
	if j == nil {
		return ""
	}
	return j.path
}

// DefaultPath is $XDG_STATE_HOME/argo-tui/actions.jsonl, or
// ~/.local/state/argo-tui/actions.jsonl when XDG_STATE_HOME is unset. The
// XDG spec defines the state directory as the place for exactly this kind
// of history: kept across runs, not configuration, not cache.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" && filepath.IsAbs(dir) {
		return filepath.Join(dir, "argo-tui", "actions.jsonl"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("no state directory: XDG_STATE_HOME and the home directory are both unset")
	}
	return filepath.Join(home, ".local", "state", "argo-tui", "actions.jsonl"), nil
}

// Record appends e as one line. It is safe for concurrent use; each line is
// written with a single write call on a file opened for append, so lines
// from two processes cannot interleave inside one another.
func (j *Journal) Record(e Entry) error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.err != nil {
		return j.err
	}
	if j.path == "" {
		return nil
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(filepath.Dir(j.path), dirMode); err != nil {
		return err
	}
	f, err := os.OpenFile(j.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, fileMode)
	if err != nil {
		return err
	}
	_, werr := f.Write(line)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}
