package core

import "context"

// ArchiveQuery is one page request to the workflow archive.
type ArchiveQuery struct {
	// Namespace narrows the list to one namespace; empty lists every
	// namespace the token may read.
	Namespace     string
	LabelSelector string
	// Continue is the opaque token of the previous page, passed back
	// verbatim.
	Continue string
	Limit    int64
}

// ArchivePage is one page of archived workflows. The items keep their raw
// objects in Resource, because the archive list is the only place a row's
// record comes from until the reader opens it.
type ArchivePage struct {
	Items    []Workflow
	Continue string
}

// ArchiveReader reads the workflow archive: workflows the controller copied
// to its database, which may be gone from the cluster. It is optional; a
// Reader that does not implement it has no archive view.
type ArchiveReader interface {
	ListArchivedWorkflows(ctx context.Context, q ArchiveQuery) (ArchivePage, error)
	// GetArchivedWorkflow returns the full archived workflow by UID, which
	// is the archive's own key.
	GetArchivedWorkflow(ctx context.Context, uid string) (Workflow, error)
}

// ArchiveDisabledMessage replaces the error a server without an archive
// returns for an archived workflow.
const ArchiveDisabledMessage = "the workflow archive is not enabled on this server"
