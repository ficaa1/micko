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

// archivePageSize and archiveCap bound one collection of the archive: the
// list reads the newest pages only and says so.
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

// openArchived opens an archived run in the detail route; esc returns to the
// archive list.
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

// archivedDetailCmd reads one archived workflow. Its reply is the live
// fetch's message, so the staleness and UID checks are shared.
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

// archivedLogNote explains an empty or failed log stream of an archived run.
const archivedLogNote = "this workflow is archived: its pods are usually gone, and its logs remain only if the workflow archived them (archiveLogs)"

// archivedLogOutcome sets what the log pane says when an archived run's
// stream ends. It reports true for a failure, which the caller must not
// overwrite. A clean end leaves a notice shown only if no line arrived.
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
