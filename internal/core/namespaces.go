package core

import "context"

// NamespaceLister reports the namespaces this session may list workflows in.
// It is optional: Argo has no namespace-list endpoint, so a Reader that
// cannot answer leaves it unimplemented.
type NamespaceLister interface {
	// ListNamespaces returns the namespaces, sorted, plus a short sanitized
	// note about how they were obtained. The note is shown to the reader, so
	// a narrowed or derived list never passes as the whole truth.
	ListNamespaces(context.Context) (names []string, note string, err error)
}
