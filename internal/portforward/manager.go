// Package portforward manages one owned kubectl port-forward process.
package portforward

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const maxOutput = 4096

type Target struct {
	Context    string
	Namespace  string
	Service    string
	RemotePort string
	LocalPort  string
}

func (t Target) validate() error {
	for name, value := range map[string]string{"context": t.Context, "namespace": t.Namespace, "service": t.Service, "remote port": t.RemotePort, "local port": t.LocalPort} {
		if value == "" || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("invalid %s", name)
		}
	}
	return nil
}

type State string

const (
	StateStarting State = "starting"
	StateReady    State = "ready"
	StateLost     State = "lost"
	StateFailed   State = "failed"
	StateStopped  State = "stopped"
)

type Event struct {
	State   State
	Message string
	Attempt int
}

type Process interface {
	StdoutPipe() (io.ReadCloser, error)
	StderrPipe() (io.ReadCloser, error)
	Start() error
	Wait() error
	Kill() error
}
type Command func(context.Context, string, ...string) Process

type Manager struct {
	target  Target
	command Command
	backoff []time.Duration
	events  chan Event
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	proc    Process
	started bool
}

func New(target Target) (*Manager, error) {
	return NewWithCommand(target, func(ctx context.Context, name string, args ...string) Process {
		return execCommand{exec.CommandContext(ctx, name, args...)}
	})
}
func NewWithCommand(target Target, command Command) (*Manager, error) {
	if err := target.validate(); err != nil {
		return nil, err
	}
	if command == nil {
		return nil, errors.New("nil command")
	}
	return &Manager{target: target, command: command, backoff: []time.Duration{100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond, time.Second}, events: make(chan Event, 16), done: make(chan struct{})}, nil
}
func (m *Manager) Events() <-chan Event { return m.events }
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.mu.Unlock()
	go m.run(ctx)
}
func (m *Manager) Close() {
	m.once.Do(func() {
		close(m.done)
		m.mu.Lock()
		if m.proc != nil {
			_ = m.proc.Kill()
		}
		m.mu.Unlock()
	})
}

func (m *Manager) run(ctx context.Context) {
	defer close(m.events)
	attempt := 0
	for {
		select {
		case <-ctx.Done():
			m.emit(Event{State: StateStopped, Message: "canceled", Attempt: attempt})
			return
		case <-m.done:
			m.emit(Event{State: StateStopped, Message: "stopped", Attempt: attempt})
			return
		default:
		}
		attempt++
		m.emit(Event{State: StateStarting, Attempt: attempt})
		p := m.command(ctx, "kubectl", "--context", m.target.Context, "--namespace", m.target.Namespace, "port-forward", "service/"+m.target.Service, m.target.LocalPort+":"+m.target.RemotePort)
		out, err := p.StdoutPipe()
		if err != nil {
			m.fail(err, attempt)
			if !m.sleep(ctx, attempt) {
				return
			}
			continue
		}
		errout, err := p.StderrPipe()
		if err != nil {
			m.fail(err, attempt)
			if !m.sleep(ctx, attempt) {
				return
			}
			continue
		}
		// Serialize Start with Close. Some Process implementations (including
		// os/exec) cannot be killed before Start initializes their Process.
		m.mu.Lock()
		if err := p.Start(); err != nil {
			m.mu.Unlock()
			m.fail(err, attempt)
			if !m.sleep(ctx, attempt) {
				return
			}
			continue
		}
		m.proc = p
		closed := false
		select {
		case <-m.done:
			closed = true
		case <-ctx.Done():
			closed = true
		default:
		}
		m.mu.Unlock()
		if closed {
			_ = p.Kill()
			_ = p.Wait()
			m.clearProc(p)
			m.emit(Event{State: StateStopped, Message: "stopped", Attempt: attempt})
			return
		}
		ready := make(chan struct{}, 1)
		lines := make(chan string, 8)
		go scan(out, lines)
		go scan(errout, lines)
		stateReady := false
		wait := make(chan error, 1)
		go func() { wait <- p.Wait() }()
	loop:
		for {
			select {
			case line := <-lines:
				if line == "" {
					continue
				}
				if len(line) > maxOutput {
					line = line[:maxOutput]
				}
				if strings.Contains(line, "Forwarding from 127.0.0.1:") || strings.Contains(line, "Forwarding from [::1]:") {
					if !stateReady {
						stateReady = true
						m.emit(Event{State: StateReady, Message: line, Attempt: attempt})
						ready <- struct{}{}
					}
				} else if strings.Contains(strings.ToLower(line), "unable to listen") || strings.Contains(strings.ToLower(line), "error") {
					m.emit(Event{State: StateFailed, Message: sanitize(line), Attempt: attempt})
				}
			case err := <-wait:
				m.clearProc(p)
				if stateReady {
					m.emit(Event{State: StateLost, Message: exitMessage(err), Attempt: attempt})
				} else {
					m.emit(Event{State: StateFailed, Message: exitMessage(err), Attempt: attempt})
				}
				break loop
			case <-ctx.Done():
				_ = p.Kill()
				<-wait
				m.clearProc(p)
				m.emit(Event{State: StateStopped, Message: "canceled", Attempt: attempt})
				return
			case <-m.done:
				_ = p.Kill()
				<-wait
				m.clearProc(p)
				m.emit(Event{State: StateStopped, Message: "stopped", Attempt: attempt})
				return
			}
		}
		if !m.sleep(ctx, attempt) {
			return
		}
		_ = ready
	}
}
func (m *Manager) fail(err error, attempt int) {
	m.emit(Event{State: StateFailed, Message: sanitize(err.Error()), Attempt: attempt})
}
func (m *Manager) emit(e Event) {
	select {
	case m.events <- e:
	case <-m.done:
	}
}
func (m *Manager) clearProc(p Process) {
	m.mu.Lock()
	if m.proc == p {
		m.proc = nil
	}
	m.mu.Unlock()
}
func (m *Manager) sleep(ctx context.Context, attempt int) bool {
	d := m.backoff[len(m.backoff)-1]
	if attempt <= len(m.backoff) {
		d = m.backoff[attempt-1]
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	case <-m.done:
		return false
	}
}
func scan(r io.Reader, ch chan<- string) {
	s := bufio.NewScanner(io.LimitReader(r, maxOutput+1))
	for s.Scan() {
		select {
		case ch <- s.Text():
		default:
		}
	}
}
func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxOutput {
		s = s[:maxOutput]
	}
	return s
}
func exitMessage(err error) string {
	if err == nil {
		return "process exited"
	}
	return sanitize(err.Error())
}

type execCommand struct{ *exec.Cmd }

func (p execCommand) Kill() error {
	if p.Cmd.Process == nil {
		return nil
	}
	return p.Cmd.Process.Kill()
}
