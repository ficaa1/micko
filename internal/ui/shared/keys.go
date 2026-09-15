// Package shared — key binding contracts (plan §2 Navigation).
//
// Key semantics frozen here so every view binds identically:
//   - arrows or j/k move; Enter opens; Esc backs out/cancels
//   - Tab changes detail tabs; / focuses local search
//   - n namespace picker; P profile picker; p phase filter; r refresh; ? help
//   - q quits only outside text entry; Ctrl-C quits globally
//
// Text-entry views must consume printable input themselves and must not
// interpret navigation keys as commands (acceptance UI-04).
package shared

// KeyContext describes what a view is currently doing, so the same key can
// mean different things in different modes (e.g. `q` outside text entry).
type KeyContext int

const (
	// KeyCtxBrowsing is the normal navigation mode.
	KeyCtxBrowsing KeyContext = iota
	// KeyCtxTextEntry means a text input has focus (search, namespace
	// entry, confirm-typing). q must not quit here.
	KeyCtxTextEntry
	// KeyCtxDialog means a modal dialog has focus; Esc cancels, default
	// action is Cancel (plan §6).
	KeyCtxDialog
)

// String implements fmt.Stringer for readable test output.
func (k KeyContext) String() string {
	switch k {
	case KeyCtxTextEntry:
		return "text-entry"
	case KeyCtxDialog:
		return "dialog"
	default:
		return "browsing"
	}
}

// QuitKeySet reports whether a key event should quit given the context.
// `q` quits only outside text entry and dialogs; Ctrl-C quits globally.
func QuitKeySet(ctrlC bool, key string, ctx KeyContext) bool {
	if ctrlC {
		return true
	}
	if ctx == KeyCtxTextEntry || ctx == KeyCtxDialog {
		return false
	}
	return key == "q"
}
