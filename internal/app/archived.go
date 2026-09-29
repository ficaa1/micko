package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/ui/archivedlist"
	"github.com/ficaa1/micko/internal/ui/kindlist"
)

// archived.go registers the workflow archive as a kind: a list of archived
// runs, each opened in the detail route from the archive by its UID.
//
// An archived run may be gone from the cluster. Its detail is read from the
// archive, not the live workflow route; it is fixed, so it is not refetched
// on the tick; actions are refused, since there is no live object to act on;
// and its logs are often gone with its pods, which the log pane says instead
// of showing an empty stream.

// archivePageSize and archiveCap bound one collection of the archive. An
// archive holds every run the controller ever copied, which is far more than
// a list is read for, so the list asks for the newest pages only and says
// that it did.
const (
	archivePageSize = 100
	archiveCap      = 300
)

// archiveResult is one collection: the rows and whether more were left.
type archiveResult struct {
	items  []core.Workflow
	capped bool
}

// newArchivedKind builds the archive kind's definition for m.
func (m *Root) newArchivedKind() *kindDef {
	m.archView = kindlist.New(archivedlist.Spec(), m.theme)
	return &kindDef{
		pane: m.archView,
		noun: "archived workflow",
		fetcher: func() (func(context.Context, string) (any, error), string) {
			archive := m.deps.archive
			if archive == nil {
				return nil, "this connection cannot read the workflow archive"
			}
			return func(ctx context.Context, ns string) (any, error) {
				var res archiveResult
				cont := ""
				for {
					page, err := archive.ListArchivedWorkflows(ctx, core.ArchiveQuery{
						Namespace: ns, Continue: cont, Limit: archivePageSize,
					})
					if err != nil {
						return nil, err
					}
					res.items = append(res.items, page.Items...)
					if page.Continue == "" || page.Continue == cont {
						return res, nil
					}
					if len(res.items) >= archiveCap {
						res.capped = true
						return res, nil
					}
					cont = page.Continue
				}
			}, ""
		},
		apply: func(items any, now time.Time) {
			res, _ := items.(archiveResult)
			m.archView.SetItems(res.items, now)
			note := ""
			if res.capped {
				note = "newest " + strconv.Itoa(len(res.items)) + " only; the archive holds more"
			}
			m.archView.SetNote(note)
		},
	}
}

// showArchived is the archived workflows kind's show.
func (m *Root) showArchived() tea.Cmd { return m.showKind(RouteArchived, "archived workflows") }

// openArchived opens an archived run in the detail route. The detail is
// read from the archive by UID, and esc returns to the archive list.
func (m *Root) openArchived(ref core.Ref) tea.Cmd {
	if m.route == RouteDetail && m.selection == ref && m.detailState.archived {
		return nil
	}
	m.detailFrom = m.route
	m.selection = ref
	m.selGen++
	m.route = RouteDetail
	m.detailState = detailState{ref: ref, loading: true, archived: true}
	m.detailView.SetArchived(true)
	m.detailView.SetLoading()
	return m.startDetailFetch()
}

// archivedDetailCmd reads one archived workflow. It answers with the same
// message as a live fetch, so staleness and UID checks are shared.
func (d deps) archivedDetailCmd(ctx context.Context, g genStamp, id uint64, ref core.Ref) func() tea.Msg {
	archive := d.archive
	return func() tea.Msg {
		if archive == nil {
			return detailLoadedMsg{genStamp: g, RequestID: id, Ref: ref,
				Err: core.NewAPIError(core.ErrUnsupported, 0, "this connection cannot read the workflow archive")}
		}
		wf, err := archive.GetArchivedWorkflow(ctx, ref.UID)
		if err != nil {
			if ctx.Err() != nil {
				return detailLoadedMsg{genStamp: g, RequestID: id, Ref: ref, Canceled: true}
			}
			ae := core.AsAPIError(err)
			if ae == nil {
				ae = core.WrapAPIError(core.ErrUnavailable, 0, "archive read failed: transport error", err)
			}
			return detailLoadedMsg{genStamp: g, RequestID: id, Ref: ref, Err: ae}
		}
		return detailLoadedMsg{genStamp: g, RequestID: id, Ref: ref, Workflow: wf}
	}
}

// archivedLogNote explains a log stream of an archived run that ended with
// nothing, or failed. The pods of an archived run are usually deleted with
// it, and only a workflow that archived its logs keeps them.
const archivedLogNote = "this workflow is archived: its pods are usually gone, and its logs remain only if the workflow archived them (archiveLogs)"

// archivedLogOutcome sets what the log pane says when a stream of an
// archived run ends. A failure is reported with the reason it most likely
// has, and it reports true so the caller does not overwrite it. A clean end
// leaves a notice the pane shows only if no line arrived: the last records
// and the end of the stream are delivered separately, so whether any
// arrived is known when the pane is drawn, not here.
func (m *Root) archivedLogOutcome(err error) bool {
	if !m.logState.archived || m.logsView == nil {
		return false
	}
	if err != nil {
		text := err.Error()
		if ae := core.AsAPIError(err); ae != nil {
			text = ae.Message
		}
		m.logsView.SetError(strings.TrimSpace(text) + " — " + archivedLogNote)
		return true
	}
	m.logsView.SetNotice("no log lines: " + archivedLogNote)
	return false
}
