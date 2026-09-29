// Package shared — key binding contracts.
//
// Key semantics frozen here so every view binds identically:
//   - arrows or j/k move; Enter opens; Esc backs out/cancels
//   - Tab changes detail tabs; / focuses local search
//   - n namespace picker; P profile picker; p phase filter; r refresh; ? help
//   - q quits only outside text entry; Ctrl-C quits globally
//
// Text-entry views must consume printable input themselves and must not
// interpret navigation keys as commands.
package shared
