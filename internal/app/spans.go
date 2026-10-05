package app

import "time"

// span ties a visible wait to the request that can end it.
type span struct {
	id    uint64
	start time.Time
}

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

// endSpan ends a span; only the request that started it can.
func (m *Root) endSpan(name string, id uint64, failed bool) {
	s, ok := m.spans[name]
	if !ok || s.id != id {
		return
	}
	delete(m.spans, name)
	m.conn.Diagnostics.EmitSpan(name, m.deps.clock.Now().Sub(s.start), failed)
}
