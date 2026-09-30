package portforward

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"
)

// fakeProcess stands in for kubectl: a test writes its output and ends it.
type fakeProcess struct {
	outR, errR *io.PipeReader
	outW, errW *io.PipeWriter
	wait       chan error
	killed     chan struct{}
	once       sync.Once
}

func newFake() *fakeProcess {
	or, ow := io.Pipe()
	er, ew := io.Pipe()
	return &fakeProcess{outR: or, errR: er, outW: ow, errW: ew, wait: make(chan error, 1), killed: make(chan struct{})}
}
func (p *fakeProcess) StdoutPipe() (io.ReadCloser, error) { return p.outR, nil }
func (p *fakeProcess) StderrPipe() (io.ReadCloser, error) { return p.errR, nil }
func (p *fakeProcess) Start() error                       { return nil }
func (p *fakeProcess) Wait() error                        { return <-p.wait }
func (p *fakeProcess) Kill() error {
	p.once.Do(func() { close(p.killed); p.outR.Close(); p.errR.Close(); p.wait <- errors.New("killed") })
	return nil
}

func (p *fakeProcess) stdout(line string) { _, _ = p.outW.Write([]byte(line + "\n")) }
func (p *fakeProcess) stderr(line string) { _, _ = p.errW.Write([]byte(line + "\n")) }

// forward is a started manager whose attempts run procs in order.
type forward struct {
	m     *Manager
	mu    sync.Mutex
	calls [][]string
}

func startForward(t *testing.T, target Target, procs ...*fakeProcess) *forward {
	t.Helper()
	f := &forward{}
	m, err := newWithCommand(target, func(_ context.Context, name string, args ...string) Process {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, append([]string{name}, args...))
		return procs[min(len(f.calls), len(procs))-1]
	})
	if err != nil {
		t.Fatal(err)
	}
	f.m = m
	m.Start(context.Background())
	t.Cleanup(m.Close)
	return f
}

// await fails unless the next lifecycle event is want.
func (f *forward) await(t *testing.T, want State) {
	t.Helper()
	select {
	case e := <-f.m.Events():
		if e.State != want {
			t.Fatalf("state = %s (%s), want %s", e.State, e.Message, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", want)
	}
}

// readyWithin is what Ready returns when waited on for at most d.
func (f *forward) readyWithin(d time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return f.m.Ready(ctx)
}

// The forward runs kubectl against the configured target and is ready, with
// an endpoint, only once kubectl announces the loopback port it bound.
func TestReadinessComesFromTheAnnouncedPort(t *testing.T) {
	cases := []struct {
		name         string
		localPort    string
		announce     string
		wantCommand  []string
		wantEndpoint string
	}{
		{"ephemeral port on IPv4", "", "Forwarding from 127.0.0.1:51234 -> 443",
			[]string{"kubectl", "--context", "prod ctx", "--namespace", "team-a", "port-forward", "service/argo", "0:443"},
			"http://127.0.0.1:51234"},
		{"fixed port on IPv6", "15443", "Forwarding from [::1]:15443 -> 443",
			[]string{"kubectl", "--context", "prod ctx", "--namespace", "team-a", "port-forward", "service/argo", "15443:443"},
			"http://[::1]:15443"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newFake()
			f := startForward(t, Target{"prod ctx", "team-a", "argo", "443", c.localPort}, p)
			f.await(t, StateStarting)
			if err := f.readyWithin(20 * time.Millisecond); err == nil {
				t.Fatal("ready before kubectl announced a port")
			}
			if got := f.m.Endpoint(); got != "" {
				t.Fatalf("endpoint before the announcement = %q", got)
			}
			p.stdout(c.announce)
			f.await(t, StateReady)
			if err := f.readyWithin(time.Second); err != nil {
				t.Fatalf("Ready after the announcement: %v", err)
			}
			if got := f.m.Endpoint(); got != c.wantEndpoint {
				t.Errorf("endpoint = %q, want %q", got, c.wantEndpoint)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if !reflect.DeepEqual(f.calls, [][]string{c.wantCommand}) {
				t.Errorf("commands = %q, want %q", f.calls, c.wantCommand)
			}
		})
	}
}

// An error line fails an attempt that has not bound a port yet; once the
// forward serves, the same kind of line is one dropped connection and the
// endpoint stays.
func TestErrorLines(t *testing.T) {
	cases := []struct {
		name         string
		ready        bool
		line         string
		want         State
		wantEndpoint string
	}{
		{"before readiness", false, "Unable to listen on port 2746: address already in use", StateFailed, ""},
		{"after readiness", true, "E0918 12:00:00.000000 1 portforward.go:409] an error occurred forwarding 51234 -> 2746",
			StateWarning, "http://127.0.0.1:51234"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newFake()
			f := startForward(t, Target{"ctx", "ns", "api", "2746", ""}, p)
			f.await(t, StateStarting)
			if c.ready {
				p.stdout("Forwarding from 127.0.0.1:51234 -> 2746")
				f.await(t, StateReady)
			}
			p.stderr(c.line)
			f.await(t, c.want)
			if got := f.m.Endpoint(); got != c.wantEndpoint {
				t.Errorf("endpoint = %q, want %q", got, c.wantEndpoint)
			}
		})
	}
}

// A forward whose process exits drops its endpoint at once, starts a new
// attempt and is ready on the port the new process announces, and a caller
// already waiting for readiness sees that recovery.
func TestAForwardRecovers(t *testing.T) {
	cases := []struct {
		name      string
		bound     bool
		wantAfter State
	}{
		{"lost after readiness", true, StateLost},
		{"failed before readiness", false, StateFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			first, second := newFake(), newFake()
			f := startForward(t, Target{"ctx", "ns", "api", "8080", ""}, first, second)
			f.await(t, StateStarting)
			waited := make(chan error, 1)
			go func() { waited <- f.readyWithin(5 * time.Second) }()
			if c.bound {
				first.stdout("Forwarding from 127.0.0.1:1111 -> 8080")
				f.await(t, StateReady)
			}

			first.wait <- errors.New("connection lost")
			f.await(t, c.wantAfter)
			if got := f.m.Endpoint(); got != "" {
				t.Fatalf("endpoint after the process exited = %q, want none", got)
			}
			f.await(t, StateStarting)
			second.stdout("Forwarding from 127.0.0.1:2222 -> 8080")
			f.await(t, StateReady)
			if err := <-waited; err != nil {
				t.Fatalf("Ready across the attempts: %v", err)
			}
			if got := f.m.Endpoint(); got != "http://127.0.0.1:2222" {
				t.Errorf("endpoint after recovery = %q, want the new port", got)
			}
		})
	}
}

// Close kills the owned process and releases anyone waiting for readiness.
func TestCloseKillsTheProcess(t *testing.T) {
	p := newFake()
	f := startForward(t, Target{"ctx", "ns", "api", "8080", ""}, p)
	f.await(t, StateStarting)
	f.m.Close()
	select {
	case <-p.killed:
	case <-time.After(2 * time.Second):
		t.Fatal("the owned process was not killed")
	}
	if err := f.readyWithin(time.Second); err == nil || err == context.DeadlineExceeded {
		t.Errorf("Ready after Close = %v, want the forward reported stopped", err)
	}
}

// A target needs every field but the local port, and no field may carry a
// line break or NUL into kubectl's arguments.
func TestTargetValidation(t *testing.T) {
	cases := []struct {
		name   string
		target Target
		ok     bool
	}{
		{"complete", Target{"ctx", "ns", "svc", "443", "8443"}, true},
		{"no local port", Target{"ctx", "ns", "svc", "443", ""}, true},
		{"newline in namespace", Target{"ctx", "ns\n", "svc", "443", ""}, false},
		{"NUL in context", Target{"c\x00tx", "ns", "svc", "443", ""}, false},
		{"no service", Target{"ctx", "ns", "", "443", ""}, false},
		{"no remote port", Target{"ctx", "ns", "svc", "", ""}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := New(c.target)
			if (err == nil) != c.ok {
				t.Errorf("New(%+v) error = %v, want accepted: %v", c.target, err, c.ok)
			}
		})
	}
}
