package journal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Each record is one JSON line with every field of the format, appended in
// order.
func TestRecordAppendsOneJSONLinePerEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "micko", "actions.jsonl")
	j := New(path)
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	first := Entry{Time: at, Profile: "dev", Server: "https://argo.test", Namespace: "ns", Name: "wf-a", UID: "uid-a", Verb: "retry", Outcome: "confirmed"}
	second := Entry{Time: at.Add(time.Second), Profile: "dev", Server: "https://argo.test", Namespace: "ns", Name: "wf-b", UID: "uid-b", Verb: "retry", Outcome: "refused", Error: "workflow UID mismatch"}
	for _, e := range []Entry{first, second} {
		if err := j.Record(e); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var lines []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("line %q is not JSON: %v", sc.Text(), err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	for _, key := range []string{"time", "profile", "server", "namespace", "name", "uid", "verb", "outcome"} {
		if _, ok := lines[0][key]; !ok {
			t.Errorf("line has no %q field: %v", key, lines[0])
		}
	}
	if lines[0]["name"] != "wf-a" || lines[1]["name"] != "wf-b" {
		t.Fatalf("lines out of order: %v", lines)
	}
	if lines[0]["time"] != "2026-09-24T12:00:00Z" || lines[1]["error"] != "workflow UID mismatch" {
		t.Fatalf("fields = %v / %v", lines[0], lines[1])
	}
	if _, ok := lines[0]["error"]; ok {
		t.Fatal("an entry with no error wrote an error field")
	}
}

// The journal names workflows and servers; it is readable by its owner only.
func TestTheJournalFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	path := filepath.Join(t.TempDir(), "actions.jsonl")
	if err := New(path).Record(Entry{Verb: "stop"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := st.Mode().Perm(); mode != 0o600 {
		t.Fatalf("mode = %o, want 600", mode)
	}
}

// A failed append is returned to the caller, and a later append to a path
// that works again succeeds: nothing is latched.
func TestAFailedAppendIsReturned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "actions.jsonl")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	j := New(path)
	if err := j.Record(Entry{Verb: "stop"}); err == nil {
		t.Fatal("appending to a directory reported success")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := j.Record(Entry{Verb: "stop"}); err != nil {
		t.Fatalf("append after the path was fixed: %v", err)
	}
}

// A nil journal is the journal of a session that has none: it records
// nothing and never fails.
func TestANilJournalRecordsNothing(t *testing.T) {
	var j *Journal
	if err := j.Record(Entry{Verb: "stop"}); err != nil {
		t.Fatalf("nil journal: %v", err)
	}
}

func TestDefaultPathFollowsXDGStateHome(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	got, err := DefaultPath()
	if err != nil || got != filepath.Join(state, "micko", "actions.jsonl") {
		t.Fatalf("path = %q, err = %v", got, err)
	}

	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)
	got, err = DefaultPath()
	if err != nil || got != filepath.Join(home, ".local", "state", "micko", "actions.jsonl") {
		t.Fatalf("fallback path = %q, err = %v", got, err)
	}

	// The XDG spec says a relative value is invalid and must be ignored.
	t.Setenv("XDG_STATE_HOME", "relative/state")
	got, _ = DefaultPath()
	if got != filepath.Join(home, ".local", "state", "micko", "actions.jsonl") {
		t.Fatalf("relative XDG_STATE_HOME was used: %q", got)
	}
}

// A journal kept under the tool's former name is appended to until a micko
// journal exists, so the rename does not split the record of writes.
func TestDefaultPathKeepsALegacyJournal(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	legacy := filepath.Join(state, "argo-tui", "actions.jsonl")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := DefaultPath(); err != nil || got != legacy {
		t.Fatalf("path = %q, err = %v; want the legacy journal %q", got, err, legacy)
	}

	current := filepath.Join(state, "micko", "actions.jsonl")
	if err := os.MkdirAll(filepath.Dir(current), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := DefaultPath(); err != nil || got != current {
		t.Fatalf("path = %q, err = %v; want the micko journal %q once it exists", got, err, current)
	}
}
