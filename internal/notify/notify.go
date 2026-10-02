// Package notify decides which workflow changes are worth telling the
// reader about and delivers them outside the terminal, so a change reaches
// someone who is looking at another window.
package notify

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ficaa1/micko/internal/config"
	"github.com/ficaa1/micko/internal/core"
)

// Notice is one notification. Group names the workflow it is about, so a
// newer notice replaces an older one for the same workflow.
type Notice struct {
	Title    string
	Subtitle string
	Body     string
	Group    string
}

// state is the phase the list shows: a running workflow parked on a manual
// gate reads Suspended.
func state(s core.Summary) string {
	if s.Suspended {
		return "Suspended"
	}
	return s.Phase
}

func notice(ref core.Ref, body string) Notice {
	return Notice{Title: ref.Name, Subtitle: ref.Namespace, Body: body, Group: "micko-" + ref.UID}
}

// Change returns the notice for a workflow that moved from before to after,
// and false when the settings ask for none.
func Change(before, after core.Summary, watched bool, n config.Notify) (Notice, bool) {
	from, to := state(before), state(after)
	if from == to {
		return Notice{}, false
	}
	switch {
	case watched && n.Watched:
		return notice(after.Ref, from+" → "+to), true
	case n.Suspended && to == "Suspended":
		return notice(after.Ref, "suspended, waiting for resume"), true
	}
	return Notice{}, false
}

// Gone is the notice for a watched workflow that no longer exists.
func Gone(ref core.Ref) Notice { return notice(ref, "deleted; no longer watched") }

// OSC9 is n as a terminal desktop-notification sequence. Terminals that
// support it post the notification; zellij forwards it to the terminal it
// runs in, or rings that terminal's bell. Control characters are dropped so
// a workflow name cannot end the sequence early.
func OSC9(n Notice) string {
	text := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, n.Title+": "+n.Body)
	return "\x1b]9;" + text + "\a"
}

// Deliver sends n by the desktop and sound methods in s.Via. The bell and
// the terminal method are the caller's: they go through the terminal's own
// output. Workflow names reach every command as arguments, never as script
// text.
func Deliver(n Notice, s config.Notify) error {
	var errs []error
	for _, via := range s.Via {
		switch via {
		case config.NotifyDesktop:
			errs = append(errs, desktop(n, s))
		case config.NotifySound:
			if runtime.GOOS != "darwin" {
				errs = append(errs, errors.New("notification sounds need macOS"))
				continue
			}
			sound := filepath.Join("/System/Library/Sounds", filepath.Base(s.Sound)+".aiff")
			errs = append(errs, exec.Command("afplay", sound).Run())
		}
	}
	return errors.Join(errs...)
}

// desktop posts n to Notification Center. With terminal-notifier installed
// the banner comes from micko's own app, and a click brings the terminal
// forward; without it osascript posts a banner that a click cannot act on.
func desktop(n Notice, s config.Notify) error {
	if runtime.GOOS != "darwin" {
		return errors.New("desktop notifications need macOS")
	}
	tool, err := exec.LookPath("terminal-notifier")
	if err != nil {
		return exec.Command("osascript",
			"-e", "on run argv",
			"-e", "display notification (item 3 of argv) with title (item 1 of argv) subtitle (item 2 of argv)",
			"-e", "end run",
			n.Title, n.Subtitle, n.Body).Run()
	}
	app, buildErr := micko(tool)
	if buildErr == nil {
		tool = app
	}
	args := []string{"-title", n.Title, "-subtitle", n.Subtitle, "-message", n.Body, "-group", n.Group}
	if id := activateID(s); id != "" {
		args = append(args, "-activate", id)
	}
	if err := exec.Command(tool, args...).Run(); err != nil {
		return err
	}
	if buildErr != nil {
		return fmt.Errorf("sent as terminal-notifier; building micko.app: %w", buildErr)
	}
	return nil
}

// activateID is the app a click on a notification brings forward: the
// configured one, else the app micko was started from. macOS sets
// __CFBundleIdentifier for every process an app launches, and zellij passes
// its environment on to its panes.
func activateID(s config.Notify) string {
	if s.Activate != "" {
		return s.Activate
	}
	return os.Getenv("__CFBundleIdentifier")
}
