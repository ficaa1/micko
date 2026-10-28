package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ficaa1/micko/internal/argo"
	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/journal"
	"github.com/ficaa1/micko/internal/ui/actions"
	"github.com/ficaa1/micko/internal/ui/namespaces"
)

func TestResourceActionJourneys(t *testing.T) {
	cases := []struct {
		name, command, key, kind, namespace, verb, fail string
		readonly, demo, hide                            bool
	}{
		{name: "template", command: "tmpl", key: "g", kind: "WorkflowTemplate", namespace: "other", verb: "submit"},
		{name: "cluster template", command: "cwftmpl", key: "g", kind: "ClusterWorkflowTemplate", namespace: "ns", verb: "submit"},
		{name: "cron run now", command: "cron", key: "g", kind: "CronWorkflow", namespace: "other", verb: "submit"},
		{name: "cron suspend", command: "cron", key: "z", kind: "CronWorkflow", namespace: "other", verb: "suspend"},
		{name: "cron resume", command: "cron", key: "u", kind: "CronWorkflow", namespace: "other", verb: "resume"},
		{name: "replacement", command: "tmpl", key: "g", kind: "WorkflowTemplate", namespace: "other", verb: "submit", fail: "uid"},
		{name: "required value", command: "tmpl", key: "g", kind: "WorkflowTemplate", namespace: "other", verb: "submit", fail: "required"},
		{name: "ambiguous response", command: "tmpl", key: "g", kind: "WorkflowTemplate", namespace: "other", verb: "submit", fail: "broken"},
		{name: "forbidden write", command: "cron", key: "z", kind: "CronWorkflow", namespace: "other", verb: "suspend", fail: "forbidden"},
		{name: "stale list", command: "tmpl", key: "g", kind: "WorkflowTemplate", namespace: "other", verb: "submit", fail: "stale"},
		{name: "read only", command: "tmpl", key: "g", kind: "WorkflowTemplate", namespace: "other", verb: "submit", readonly: true},
		{name: "demo", command: "cron", key: "g", kind: "CronWorkflow", namespace: "other", verb: "submit", demo: true},
		{name: "redacted edit", command: "tmpl", key: "g", kind: "WorkflowTemplate", namespace: "other", verb: "submit", hide: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			writes := 0
			lastBody := ""
			lastPath := ""
			suspended := c.verb == "resume"
			reads := 0
			resource := func(uid string) string {
				ns := c.namespace
				if c.kind == "ClusterWorkflowTemplate" {
					ns = ""
				}
				spec := `"arguments":{"parameters":[{"name":"message","value":"sensitive-default"}]}`
				if c.fail == "required" {
					spec = `"arguments":{"parameters":[{"name":"message"}]}`
				}
				if c.kind == "CronWorkflow" {
					spec = fmt.Sprintf(`"schedules":["0 * * * *"],"suspend":%t,"workflowSpec":{%s}`, suspended, spec)
				}
				return fmt.Sprintf(`{"metadata":{"name":"source","namespace":%q,"uid":%q},"spec":{%s}}`, ns, uid, spec)
			}
			workflow := `{"metadata":{"name":"new-run","namespace":"` + c.namespace + `","uid":"new-uid"},"status":{"phase":"Pending"}}`
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Method != "GET" {
					writes++
					lastPath = r.URL.Path
					var raw json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
						t.Error(err)
					}
					lastBody = string(raw)
					if c.fail == "forbidden" {
						w.WriteHeader(403)
						fmt.Fprint(w, `{"message":"sensitive-default"}`)
						return
					}
					if c.fail == "broken" {
						fmt.Fprint(w, `{"broken":`)
						return
					}
					if c.verb == "submit" {
						fmt.Fprint(w, workflow)
					} else {
						suspended = c.verb == "suspend"
						fmt.Fprint(w, resource("source-uid"))
					}
					return
				}
				if r.URL.Path == "/api/v1/info" {
					fmt.Fprint(w, `{}`)
					return
				}
				if strings.Contains(r.URL.Path, "/workflows/") {
					if strings.HasSuffix(r.URL.Path, "/new-run") {
						fmt.Fprint(w, workflow)
					} else {
						fmt.Fprint(w, `{"items":[]}`)
					}
					return
				}
				if strings.HasSuffix(r.URL.Path, "/source") {
					reads++
					uid := "source-uid"
					if c.fail == "uid" {
						uid = "replacement"
					}
					fmt.Fprint(w, resource(uid))
					return
				}
				fmt.Fprintf(w, `{"items":[%s]}`, resource("source-uid"))
			}))
			defer srv.Close()
			client, err := argo.NewClient(argo.Options{Server: srv.URL, TokenFn: func() (string, error) { return "", nil }})
			if err != nil {
				t.Fatal(err)
			}
			m := armedRoot(client, actions.Options{ReadOnly: c.readonly, Demo: c.demo, Profile: "test"})
			m.SetRedactValues(c.hide)
			m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
			state := t.TempDir()
			t.Setenv("XDG_STATE_HOME", state)
			m.SetJournal(journal.Open())
			runLine(m, c.command)
			if c.fail == "stale" {
				m.kindStates[m.route].lastErr = core.NewAPIError(core.ErrUnavailable, 0, "stale")
			}
			runLine(m, "actions")
			if c.readonly || c.demo || c.fail == "stale" {
				if m.actionView.State() != actions.StateUnavailable {
					t.Fatal("write should be blocked")
				}
				typeKeys(m, c.key+"y")
				if writes != 0 {
					t.Fatal("blocked action sent")
				}
				return
			}
			typeKeys(m, c.key)
			if m.actionView.State() != actions.StateConfirm {
				t.Fatal("confirmation missing", screen(m))
			}
			if c.hide && strings.Contains(screen(m), "sensitive-default") {
				t.Fatal("value revealed")
			}
			if c.verb == "submit" && c.fail != "required" {
				typeKeys(m, "e")
				m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
				typeKeys(m, "new-secret")
				if c.hide && strings.Contains(screen(m), "new-secret") {
					t.Fatal("edit revealed")
				}
				pressKey(m, tea.KeyEnter)
			}
			deliver(m, typeKeys(m, "y"))
			if c.fail == "required" {
				if writes != 0 || !strings.Contains(screen(m), "required") {
					t.Fatal("missing required parameter was sent")
				}
				return
			}
			if c.fail == "uid" {
				if writes != 0 || !strings.Contains(screen(m), "identity changed") {
					t.Fatal("replacement mutated", screen(m))
				}
				return
			}
			if writes != 1 || reads < 1 {
				t.Fatalf("writes=%d reads=%d", writes, reads)
			}
			typeKeys(m, "y")
			if writes != 1 {
				t.Fatal("confirmation sent twice")
			}
			if c.fail == "broken" || c.fail == "forbidden" {
				if m.actionView.State() != actions.StateOutcome || !strings.Contains(screen(m), "UNKNOWN") {
					t.Fatal("ambiguous action claimed success", screen(m))
				}
			} else if c.verb == "submit" {
				if m.route != RouteDetail || m.selection != (core.Ref{Namespace: c.namespace, Name: "new-run", UID: "new-uid"}) {
					t.Fatal("new workflow did not open", m.selection)
				}
				wantPath := "/api/v1/workflows/" + c.namespace + "/submit"
				if lastPath != wantPath || !strings.Contains(lastBody, `"resourceKind":"`+c.kind+`"`) || !strings.Contains(lastBody, `"parameters":["message=new-secret"]`) {
					t.Fatal("wrong submission", lastPath, lastBody)
				}
			} else {
				if m.actionView.State() != actions.StateIdle || suspended != (c.verb == "suspend") {
					t.Fatal("cron action did not settle")
				}
				if lastPath != "/api/v1/cron-workflows/other/source/"+c.verb {
					t.Fatal(lastPath)
				}
			}
			data, err := os.ReadFile(filepath.Join(state, "micko", "actions.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "secret") || strings.Contains(string(data), "sensitive-default") || !strings.Contains(string(data), `"kind":"`+c.kind+`"`) {
				t.Fatal("unsafe or ambiguous journal", string(data))
			}
		})
	}
}

func TestResourceConfirmationFitsTerminal(t *testing.T) {
	for _, height := range []int{18, 24, 40} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			m, f := demoRoot(t)
			m.actionOpts = actions.Options{AllowActions: true}
			m.Update(tea.WindowSizeMsg{Width: 80, Height: height})
			args := make([]core.Argument, 30)
			for i := range args {
				args[i] = core.Argument{Name: fmt.Sprintf("parameter-%02d", i), HasValue: true, Value: "value"}
			}
			f.WorkflowTemplates = []core.WorkflowTemplate{{Namespace: "demo", Name: "template", UID: "uid", Arguments: args}}
			runLine(m, "tmpl")
			deliver(m, typeKeys(m, "ag"))
			for range 29 {
				typeKeys(m, "j")
			}
			view := screen(m)
			if !strings.Contains(view, "parameter-29") || !strings.Contains(view, "[y] Confirm") || !strings.Contains(view, "e edit") {
				t.Fatal("confirmation is inaccessible", view)
			}
			typeKeys(m, "e")
			typeKeys(m, "x")
			pressKey(m, tea.KeyEscape)
			pressKey(m, tea.KeyEscape)
			if m.actionView.State() != actions.StateIdle {
				t.Fatal("cancel did not close")
			}
		})
	}
}

func TestResourceReplyCannotCrossScope(t *testing.T) {
	for _, c := range []string{"namespace switch", "connection lost before send"} {
		t.Run(c, func(t *testing.T) {
			m, f := demoRoot(t)
			m.actionOpts = actions.Options{AllowActions: true}
			runLine(m, "tmpl")
			typeKeys(m, "ag")
			cmd := typeKeys(m, "y")
			msgs := runCmd(cmd)
			if len(msgs) != 1 {
				t.Fatal("no intent")
			}
			before := len(f.Workflows)
			if c == "connection lost before send" {
				m.Update(ConnectionStateMsg{Conn: m.connGen, Ready: false})
				_, write := m.Update(msgs[0])
				deliver(m, write)
				if len(f.Workflows) != before || m.actionView.State() != actions.StateOutcome || !strings.Contains(screen(m), "REFUSED") {
					t.Fatal("lost connection left an action pending or sent it", screen(m))
				}
			} else {
				_, write := m.Update(msgs[0])
				result := runCmd(write)
				m.Update(namespaces.SwitchMsg{Namespace: "elsewhere"})
				for _, msg := range result {
					m.Update(msg)
				}
				if m.route != RouteTemplates || m.selection.UID != "" || m.actionView.State() != actions.StateIdle {
					t.Fatal("stale result reopened workflow")
				}
			}
			m.quit()
		})
	}
}
