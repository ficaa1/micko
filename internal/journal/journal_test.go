package journal

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// openAt returns the journal Open builds with XDG_STATE_HOME set to a fresh
// directory, and the file it appends to.
func openAt(t *testing.T) (*Journal, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return Open(), filepath.Join(dir, "micko", "actions.jsonl")
}

// Each record is one JSON line in the file format, appended in order, with
// the error field only when there is an error.
func TestRecordAppendsOneJSONLinePerEntry(t *testing.T) {
	j, path := openAt(t)
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for _, e := range []Entry{
		{Time: at, Profile: "dev", Server: "https://argo.test", Namespace: "ns", Name: "wf-a", UID: "uid-a", Verb: "retry", Outcome: "confirmed"},
		{Time: at.Add(time.Second), Profile: "dev", Server: "https://argo.test", Namespace: "ns", Name: "wf-b", UID: "uid-b", Verb: "retry", Outcome: "refused", Error: "workflow UID mismatch"},
	} {
		if err := j.Record(e); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"time":"2026-09-24T12:00:00Z","profile":"dev","server":"https://argo.test","namespace":"ns","name":"wf-a","uid":"uid-a","verb":"retry","outcome":"confirmed"}` + "\n" +
		`{"time":"2026-09-24T12:00:01Z","profile":"dev","server":"https://argo.test","namespace":"ns","name":"wf-b","uid":"uid-b","verb":"retry","outcome":"refused","error":"workflow UID mismatch"}` + "\n"
	if string(got) != want {
		t.Errorf("journal =\n%s\nwant\n%s", got, want)
	}
}

// The journal names workflows and servers, so the file and the directory it
// creates are readable by their owner only.
func TestTheJournalIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	j, path := openAt(t)
	if err := j.Record(Entry{Verb: "stop"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if mode := st.Mode().Perm(); mode != want {
			t.Errorf("%s mode = %o, want %o", p, mode, want)
		}
	}
}

// A failed append is returned to the caller, and a later append to a path
// that works again succeeds: nothing is latched.
func TestAFailedAppendIsReturned(t *testing.T) {
	j, path := openAt(t)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
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

// A journal with no state directory reports it on every append; a nil one,
// which a session without a journal holds, records nothing and never fails.
func TestAJournalWithoutAFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	if err := Open().Record(Entry{Verb: "stop"}); err == nil {
		t.Error("a journal with no state directory reported success")
	}
	var j *Journal
	if err := j.Record(Entry{Verb: "stop"}); err != nil {
		t.Errorf("nil journal: %v", err)
	}
}

// The journal lives under an absolute XDG_STATE_HOME, else under the home
// directory's .local/state.
func TestDefaultPath(t *testing.T) {
	state, home := t.TempDir(), t.TempDir()
	cases := []struct {
		name string
		xdg  string
		want string
	}{
		{"XDG_STATE_HOME", state, filepath.Join(state, "micko", "actions.jsonl")},
		{"unset", "", filepath.Join(home, ".local", "state", "micko", "actions.jsonl")},
		{"relative XDG_STATE_HOME is ignored", "relative/state", filepath.Join(home, ".local", "state", "micko", "actions.jsonl")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", c.xdg)
			t.Setenv("HOME", home)
			got, err := DefaultPath()
			if err != nil || got != c.want {
				t.Errorf("DefaultPath() = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}
