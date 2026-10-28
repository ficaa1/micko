package detail

import (
	"strings"
	"testing"
	"time"

	"github.com/ficaa1/micko/internal/core"
	"github.com/ficaa1/micko/internal/testkit"
	"github.com/ficaa1/micko/internal/ui/shared"
)

func comparisonFixture(name string) core.Workflow {
	wf := testkit.FixtureDAGWorkflow("ns", name)
	start, end := testkit.FixtureEpoch.Add(-10*time.Minute), testkit.FixtureEpoch.Add(-9*time.Minute)
	wf.Summary.StartedAt = &start
	wf.Summary.FinishedAt = &end
	nodes := map[string]core.Node{}
	for id, n := range wf.Nodes {
		n.ID = name + "-" + id
		if n.BoundaryID != "" {
			n.BoundaryID = name + "-" + n.BoundaryID
		}
		n.Children = append([]string(nil), n.Children...)
		for i, child := range n.Children {
			n.Children[i] = name + "-" + child
		}
		n.TemplateName = "work"
		if id == "root" {
			n.TemplateName = "pipeline"
		}
		n.StartedAt = &start
		n.FinishedAt = &end
		nodes[n.ID] = n
	}
	wf.Nodes = nodes
	return wf
}

func TestComparisonShowsChangesAndAmbiguity(t *testing.T) {
	for _, c := range []struct {
		name                                     string
		unavailable, fanout, duplicate, external bool
		want                                     string
	}{
		{name: "stable positions", want: "+30s"},
		{name: "unavailable nodes", unavailable: true, want: "Step comparison unavailable"},
		{name: "fanout", fanout: true, want: "Ambiguous"},
		{name: "duplicate position", duplicate: true, want: "baseline 1 nodes, selected 2 nodes"},
		{name: "external template identity unavailable", external: true, want: "Ambiguous"},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, width := range []int{30, 80, 140} {
				for _, height := range []int{8, 24, 60} {
					before, after := comparisonFixture("before"), comparisonFixture("after")
					before.Summary.Phase = "Succeeded"
					wholeFinish := after.Summary.FinishedAt.Add(2 * time.Minute)
					after.Summary.FinishedAt = &wholeFinish
					before.Summary.Arguments = []core.Argument{{Name: "token", Value: "secret-before", HasValue: true}, {Name: "removed", Value: "secret-removed", HasValue: true}, {Name: "mode", Value: "(unset)", HasValue: true}}
					after.Summary.Arguments = []core.Argument{{Name: "token", Value: "secret-after", HasValue: true}, {Name: "added", Value: "secret-added", HasValue: true}, {Name: "mode"}}
					for id, n := range before.Nodes {
						n.Phase = "Succeeded"
						before.Nodes[id] = n
					}
					n := after.Nodes["after-task-a"]
					finish := n.FinishedAt.Add(30 * time.Second)
					n.FinishedAt = &finish
					n.Outputs.Result = "secret-output"
					after.Nodes[n.ID] = n
					delete(after.Nodes, "after-task-c")
					root := after.Nodes["after-root"]
					root.Children = []string{"after-task-a", "after-task-b", "after-task-retry", "after-added"}
					after.Nodes[root.ID] = root
					after.Nodes["after-added"] = core.Node{ID: "after-added", Name: "after.added", DisplayName: "added-step", BoundaryID: root.ID, Type: "Pod", TemplateName: "extra", Phase: "Succeeded"}
					attempt := after.Nodes["after-retry-2"]
					attempt.ID = "after-retry-3"
					attempt.DisplayName = "task-retry(2)"
					after.Nodes[attempt.ID] = attempt
					retry := after.Nodes["after-task-retry"]
					retry.Children = append(retry.Children, attempt.ID)
					after.Nodes[retry.ID] = retry
					if c.unavailable {
						before.NodesAvailable = false
					}
					if c.fanout {
						n := after.Nodes["after-task-a"]
						n.DisplayName = "task-a(0:secret-fanout)"
						after.Nodes[n.ID] = n
					}
					if c.duplicate {
						n := after.Nodes["after-task-a"]
						n.ID = "duplicate"
						after.Nodes[n.ID] = n
					}
					if c.external {
						n := after.Nodes["after-task-a"]
						n.TemplateName = ""
						n.TemplateRefTemplate = "external"
						after.Nodes[n.ID] = n
					}
					m := New()
					m.SetTheme(shared.NewTheme(true))
					m.SetRedactByDefault(true)
					m.SetSize(width, height)
					m.SetWorkflow(after, testkit.FixtureEpoch)
					press(m, "C")
					m.StartCompare()
					m.SetComparison(&before, "Complete live search; archive not searched.")
					raw := strings.Join(m.RawLines(), "\n")
					for _, want := range []string{c.want, "Succeeded → Failed", "Changed input parameters", "token changed", "removed missing", "added added", "mode changed", "+2m00s"} {
						if !strings.Contains(raw, want) {
							t.Fatalf("%s %dx%d lacks %q:\n%s", c.name, width, height, want, raw)
						}
					}
					if c.fanout || c.external {
						target := "task-a(instances) [Pod"
						if c.external {
							target = "task-a [Pod · external]"
						}
						at := strings.Index(raw, target)
						if at < 0 {
							t.Fatalf("missing ambiguous position %s: %s", target, raw)
						}
						card := strings.SplitN(raw[at:], "\n\n", 2)[0]
						if !strings.Contains(card, "no individual alignment") || strings.Contains(card, "duration") {
							t.Fatalf("fabricated alignment: %s", card)
						}
					}
					if strings.Contains(raw, "secret-") || strings.Contains(raw, "\x1b") {
						t.Fatalf("redacted report leaks: %s", raw)
					}
					if !c.unavailable {
						for _, want := range []string{"added-step", "missing", "1 → 2", "no individual alignment"} {
							if !strings.Contains(raw, want) {
								t.Fatalf("missing %q: %s", want, raw)
							}
						}
					} else if strings.Contains(raw, "added-step") {
						t.Fatal("unavailable map fabricated missing nodes")
					}
					for _, line := range m.BodyLines() {
						if cellWidth(line) > width {
							t.Fatalf("overwide: %q", line)
						}
					}
					press(m, "G")
					if m.scrollLines() > m.viewRows() && m.cmp.top == 0 {
						t.Fatal("comparison does not scroll")
					}
					press(m, "home")
					if m.cmp.top != 0 {
						t.Fatal("home does not scroll")
					}
					press(m, "v")
					revealed := strings.Join(m.RawLines(), "\n")
					if !strings.Contains(revealed, "secret-after") {
						t.Fatal("v did not reveal parameters")
					}
					if strings.Contains(revealed, "secret-output") || strings.Contains(revealed, "secret-fanout") {
						t.Fatal("comparison exposes output or fanout values")
					}
					press(m, "esc")
					if m.Section() != "nodes" {
						t.Fatal("escape did not return to previous section")
					}
					other := after
					other.Summary.Ref.Namespace = "other"
					m.SetWorkflow(other, testkit.FixtureEpoch)
					press(m, "C")
					if strings.Contains(strings.Join(m.RawLines(), "\n"), "before") {
						t.Fatal("identity change kept baseline")
					}
				}
			}
		})
	}
}
