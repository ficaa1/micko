package app

import "time"

// span is one wait the reader sees: started by a key or a connection, ended
// by the reply of request id.
type span struct {
	id    uint64
	start time.Time
}

// beginSpan starts span name on the request now in flight for purpose. A
// later begin of the same name replaces it.
func (m *Root) beginSpan(name, purpose string) {
	if m.conn == nil || m.conn.Diagnostics == nil {
		return
	}
	m.mu.Lock()
	op, ok := m.inflight[purpose]
	m.mu.Unlock()
	if !ok {
		return
	}
	if m.spans == nil {
		m.spans = map[string]span{}
	}
	m.spans[name] = span{id: op.id, start: m.deps.clock.Now()}
}

// endSpan reports span name when request id is the one it waits for. A
// superseded or canceled request never reaches here with that id.
func (m *Root) endSpan(name string, id uint64, failed bool) {
	s, ok := m.spans[name]
	if !ok || s.id != id {
		return
	}
	delete(m.spans, name)
	m.conn.Diagnostics.EmitSpan(name, m.deps.clock.Now().Sub(s.start), failed)
}
