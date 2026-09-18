package portforward

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

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
func (p *fakeProcess) ready() {
	_, _ = p.outW.Write([]byte("Forwarding from 127.0.0.1:1234 -> 8080\n"))
}
func TestManagerReadinessAndCleanup(t *testing.T) {
	p := newFake()
	m, err := NewWithCommand(Target{"ctx", "ns", "api", "8080", "1234"}, func(context.Context, string, ...string) Process { return p })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	awaitState(t, m.Events(), StateStarting)
	p.ready()
	awaitState(t, m.Events(), StateReady)
	cancel()
	awaitState(t, m.Events(), StateStopped)
	select {
	case <-p.killed:
	case <-time.After(time.Second):
		t.Fatal("owned process was not killed")
	}
}
func TestManagerDoesNotReportReadyBeforeOwnedEndpoint(t *testing.T) {
	p := newFake()
	m, err := NewWithCommand(Target{"ctx", "ns", "api", "8080", "0"}, func(context.Context, string, ...string) Process { return p })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	awaitState(t, m.Events(), StateStarting)
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer readyCancel()
	if err := m.Ready(readyCtx); err == nil {
		t.Fatal("ready before endpoint")
	}
	if got := m.Endpoint(); got != "" {
		t.Fatalf("endpoint before ready: %q", got)
	}
	p.ready()
	awaitState(t, m.Events(), StateReady)
	if got := m.Endpoint(); got != "http://127.0.0.1:1234" {
		t.Fatalf("endpoint=%q", got)
	}
}

func TestManagerUsesExplicitTargetAndRecovers(t *testing.T) {
	var got []string
	var gotMu sync.Mutex
	n := 0
	p1 := newFake()
	p2 := newFake()
	m, err := NewWithCommand(Target{"prod ctx", "team-a", "argo", "443", "15443"}, func(_ context.Context, name string, args ...string) Process {
		gotMu.Lock()
		got = append([]string{name}, args...)
		n++
		call := n
		gotMu.Unlock()
		if call == 1 {
			return p1
		}
		return p2
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	awaitState(t, m.Events(), StateStarting)
	p1.ready()
	awaitState(t, m.Events(), StateReady)
	p1.wait <- errors.New("connection lost")
	awaitState(t, m.Events(), StateLost)
	awaitState(t, m.Events(), StateStarting)
	cancel()
	awaitState(t, m.Events(), StateStopped)
	gotMu.Lock()
	args := append([]string(nil), got...)
	gotMu.Unlock()
	if args[0] != "kubectl" || args[1] != "--context" || args[2] != "prod ctx" || args[3] != "--namespace" || args[4] != "team-a" {
		t.Fatalf("unexpected command %v", args)
	}
}
func TestTargetRejectsNewline(t *testing.T) {
	if _, err := NewWithCommand(Target{"ctx", "ns\n", "svc", "1", "2"}, func(context.Context, string, ...string) Process { return nil }); err == nil {
		t.Fatal("expected validation error")
	}
}
func awaitState(t *testing.T, ch <-chan Event, want State) {
	t.Helper()
	select {
	case e := <-ch:
		if e.State != want {
			t.Fatalf("state=%s want=%s (%s)", e.State, want, e.Message)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", want)
	}
}

// TestManagerKeepsServingAfterConnectionErrorLine pins that an error line from
// a forward that is still listening is a warning, not a failure. kubectl
// prints one for every single connection it drops, and a consumer that reads
// it as a lost transport blocks while the forward still carries requests.
func TestManagerKeepsServingAfterConnectionErrorLine(t *testing.T) {
	p := newFake()
	m, err := NewWithCommand(Target{"ctx", "ns", "api", "8080", "0"}, func(context.Context, string, ...string) Process { return p })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	awaitState(t, m.Events(), StateStarting)
	p.ready()
	awaitState(t, m.Events(), StateReady)
	_, _ = p.errW.Write([]byte("E0918 12:00:00.000000 1 portforward.go:409] an error occurred forwarding 51234 -> 2746\n"))
	awaitState(t, m.Events(), StateWarning)
	if got := m.Endpoint(); got != "http://127.0.0.1:1234" {
		t.Fatalf("endpoint after a dropped connection = %q, want the owned one", got)
	}
}

// TestManagerReportsErrorLineBeforeReadinessAsFailure pins the other half:
// until a port is bound, an error line is the attempt failing.
func TestManagerReportsErrorLineBeforeReadinessAsFailure(t *testing.T) {
	p := newFake()
	m, err := NewWithCommand(Target{"ctx", "ns", "api", "8080", "2746"}, func(context.Context, string, ...string) Process { return p })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.Start(ctx)
	awaitState(t, m.Events(), StateStarting)
	_, _ = p.errW.Write([]byte("Unable to listen on port 2746: address already in use\n"))
	awaitState(t, m.Events(), StateFailed)
}
