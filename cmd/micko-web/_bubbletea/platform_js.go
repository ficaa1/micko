//go:build js

package tea

// make web adds this file to a copy of charm.land/bubbletea/v2 to build
// cmd/micko-web: bubbletea ships no js platform file. The browser
// page hands the program its input and output and sends every size as a
// WindowSizeMsg, so there is no terminal to put in raw mode or to watch.

func (p *Program) listenForResize(done chan struct{}) { close(done) }

func (p *Program) initInput() error { return nil }

const suspendSupported = false

func suspendProcess() {}
