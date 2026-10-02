//go:build integration

package integration

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// Captured text is independent of where the PTY splits its reads.
func TestPTYScreenReadBoundaries(t *testing.T) {
	cases := []struct {
		name, raw, want string
	}{
		{"style", "?\x1b[22;2m help\x1b[0m", "? help"},
		{"OSC BEL", "a\x1b]0;title\a b", "a b"},
		{"OSC ST", "a\x1b]0;title\x1b\\ b", "a b"},
		{"UTF-8", "┌ ✗ Failed", "┌ ✗ Failed"},
		{"cursor", "a\x1b[5Gz", "a   z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for split := 1; split < len(c.raw); split++ {
				t.Run(fmt.Sprint(split), func(t *testing.T) {
					reader, writer, err := os.Pipe()
					if err != nil {
						t.Fatal(err)
					}
					p := &PTYProcess{master: reader, done: make(chan struct{})}
					go p.readLoop()
					t.Cleanup(func() {
						writer.Close()
						reader.Close()
						<-p.done
					})
					for i, chunk := range []string{c.raw[:split], c.raw[split:]} {
						if _, err := writer.WriteString(chunk); err != nil {
							t.Fatal(err)
						}
						deadline := time.Now().Add(time.Second)
						wantLen := split
						if i == 1 {
							wantLen = len(c.raw)
						}
						for len(p.Raw()) < wantLen {
							if time.Now().After(deadline) {
								t.Fatal("capture did not consume chunk")
							}
							time.Sleep(time.Millisecond)
						}
						_ = p.Screen()
					}
					if got := p.Screen(); got != c.want {
						t.Fatalf("Screen() = %q, want %q", got, c.want)
					}
					p.ClearScreen()
					if p.Screen() != "" || p.Raw() != "" {
						t.Fatal("ClearScreen retained captured output")
					}
				})
			}
		})
	}
}
