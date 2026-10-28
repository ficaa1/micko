package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSavedViewPaletteRoundTrip(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "namespace", true: "all namespaces"}[all], func(t *testing.T) {
			state := t.TempDir()
			t.Setenv("XDG_STATE_HOME", state)
			conn := &fakeConnector{}
			m := profileRoot(t, conn)
			runLine(m, "profile dev")
			runLine(m, "ns reports")
			if all {
				runLine(m, "all")
			}
			typeKeys(m, "/phase=failed")
			pressKey(m, tea.KeyEnter)
			typeKeys(m, "ss")
			expected := m.listView.SavedView()
			runLine(m, "view-save nightly reports")
			if !strings.Contains(m.flash, "saved view:") {
				t.Fatal(m.flash)
			}
			path := filepath.Join(state, "micko", "views.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "token") || strings.Contains(string(data), "nodes") {
				t.Fatal("resource or credential persisted")
			}
			if runtime.GOOS != "windows" {
				info, _ := os.Stat(path)
				if info.Mode().Perm() != 0600 {
					t.Fatalf("mode %v", info.Mode())
				}
			}
			m.quit()
			next := profileRoot(t, conn)
			runLine(next, "profile prod")
			typeKeys(next, " ")
			stale := runCmd(next.startListGeneration())
			runLine(next, "views")
			if !strings.Contains(next.flash, "nightly reports") {
				t.Fatal(next.flash)
			}
			if got := strings.Join(next.paletteArgs("view"), ","); got != "nightly reports" {
				t.Fatal(got)
			}
			runLine(next, "view nightly reports")
			if next.profileCurrent != "dev" || next.deps.namespace != "reports" || next.deps.allNamespaces != all || next.listView.SavedView() != expected {
				t.Fatalf("restored %s/%s all=%v view=%+v, want %+v", next.profileCurrent, next.deps.namespace, next.deps.allNamespaces, next.listView.SavedView(), expected)
			}
			if next.selection.UID != "" || len(next.listView.Marked()) != 0 || next.hasInflight("detail") {
				t.Fatal("old selection, marks or detail survived")
			}
			connections := len(conn.asked)
			runLine(next, "view nightly reports")
			if len(conn.asked) != connections+1 || next.deps.namespace != "reports" {
				t.Fatal("loading a view must reconnect even in its current profile")
			}
			count := len(next.listState.items)
			for _, msg := range stale {
				next.Update(msg)
			}
			if len(next.listState.items) != count {
				t.Fatal("stale profile rows accepted")
			}
			runLine(next, "view-delete nightly reports")
			runLine(next, "views")
			if !strings.Contains(next.flash, "no saved views") {
				t.Fatal(next.flash)
			}
		})
	}
}

func TestSavedViewErrorsPreserveSessionAndState(t *testing.T) {
	for _, c := range []struct{ name, content, command, want string }{
		{"corrupt", "{broken", "view-save ops", "corrupt"},
		{"future format", `{"version":2,"views":{}}`, "view-delete ops", "unsupported"},
		{"missing profile", `{"version":1,"views":{"ops":{"profile":"removed","namespace":"ns","phase":"All","sort":"phase"}}}`, "view ops", "unavailable"},
		{"invalid query", `{"version":1,"views":{"ops":{"profile":"dev","namespace":"ns","phase":"All","sort":"wrong"}}}`, "view ops", "sort"},
		{"missing name", `{"version":1,"views":{}}`, "view missing", "no saved view"},
		{"busy write", `{"version":1,"views":{}}`, "view-save ops", "locked"},
	} {
		t.Run(c.name, func(t *testing.T) {
			state := t.TempDir()
			t.Setenv("XDG_STATE_HOME", state)
			dir := filepath.Join(state, "micko")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "views.json")
			if err := os.WriteFile(path, []byte(c.content), 0600); err != nil {
				t.Fatal(err)
			}
			if c.name == "busy write" {
				if err := os.Mkdir(path+".lock", 0700); err != nil {
					t.Fatal(err)
				}
			}
			m := profileRoot(t, &fakeConnector{})
			runLine(m, "profile dev")
			gen := m.connGen
			runLine(m, c.command)
			if !strings.Contains(m.flash, c.want) || m.connGen != gen || m.profileCurrent != "dev" {
				t.Fatalf("flash=%q gen=%d profile=%q", m.flash, m.connGen, m.profileCurrent)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != c.content {
				t.Fatal("existing state changed", err)
			}
		})
	}
	t.Run("demo", func(t *testing.T) {
		state := t.TempDir()
		t.Setenv("XDG_STATE_HOME", state)
		m, _ := demoRoot(t)
		m.actionOpts.Demo = true
		runLine(m, "view-save demo")
		if !strings.Contains(m.flash, "disabled") {
			t.Fatal(m.flash)
		}
		if _, err := os.Stat(filepath.Join(state, "micko")); !os.IsNotExist(err) {
			t.Fatal("demo created state")
		}
	})
}
