package core

import "context"

// NamespaceLister reports the namespaces this session may list workflows in.
//
// It is optional on purpose. Argo Workflows has no "list namespaces" endpoint,
// so an implementation answers from what the server will tell it, and a
// Reader that cannot answer simply does not implement this interface. The UI
// then falls back to the namespaces the profile names and to a typed one.
type NamespaceLister interface {
	// ListNamespaces returns the namespaces, sorted, plus a short sanitized
	// note about how they were obtained. The note is shown to the reader, so
	// a narrowed or derived list never passes as the whole truth.
	ListNamespaces(context.Context) (names []string, note string, err error)
}
