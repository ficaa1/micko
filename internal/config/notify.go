package config

import (
	"fmt"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// NotifyVia is one way a notification reaches the reader.
type NotifyVia string

const (
	// NotifyDesktop posts to the macOS Notification Center.
	NotifyDesktop NotifyVia = "desktop"
	// NotifySound plays a macOS system sound.
	NotifySound NotifyVia = "sound"
	// NotifyBell rings the terminal bell. Alacritty bounces its Dock icon
	// on a bell while its window is in the background.
	NotifyBell NotifyVia = "bell"
	// NotifyTerminal sends the terminal an OSC 9 notification. zellij
	// forwards it to the terminal it runs in.
	NotifyTerminal NotifyVia = "terminal"
)

// DefaultNotifySound is the system sound the sound method plays.
const DefaultNotifySound = "Glass"

// Notify is what micko tells the reader about, and how.
type Notify struct {
	// Suspended notifies when any listed workflow becomes suspended.
	Suspended bool
	// Watched notifies on every phase change of a workflow watched with W.
	Watched bool
	Via     []NotifyVia
	// Sound is the name of a sound in /System/Library/Sounds.
	Sound string
	// Activate is the bundle identifier of the app a click on a desktop
	// notification brings forward. Empty means the app micko runs in.
	Activate string
}

// notifyFile is the notifications section as written. Watched is a pointer
// so that leaving it out keeps it on: watching a workflow is already the
// request to hear about it.
type notifyFile struct {
	Suspended bool     `yaml:"suspended,omitempty"`
	Watched   *bool    `yaml:"watched,omitempty"`
	Via       []string `yaml:"via,omitempty"`
	Sound     string   `yaml:"sound,omitempty"`
	Activate  string   `yaml:"activate,omitempty"`
}

// DefaultNotify is the setting with no notifications section: watched
// workflows notify, through the desktop and the bell on macOS and the bell
// elsewhere.
func DefaultNotify() Notify {
	via := []NotifyVia{NotifyBell}
	if runtime.GOOS == "darwin" {
		via = []NotifyVia{NotifyDesktop, NotifyBell}
	}
	return Notify{Watched: true, Via: via, Sound: DefaultNotifySound}
}

func (f *notifyFile) parse() (Notify, error) {
	n := DefaultNotify()
	if f == nil {
		return n, nil
	}
	n.Suspended = f.Suspended
	if f.Watched != nil {
		n.Watched = *f.Watched
	}
	n.Activate = strings.TrimSpace(f.Activate)
	if s := strings.TrimSpace(f.Sound); s != "" {
		n.Sound = s
	}
	if f.Via != nil {
		n.Via = nil
		for _, v := range f.Via {
			switch via := NotifyVia(strings.ToLower(strings.TrimSpace(v))); via {
			case NotifyDesktop, NotifySound, NotifyBell, NotifyTerminal:
				n.Via = append(n.Via, via)
			default:
				return DefaultNotify(), fmt.Errorf("notifications.via %q: want desktop, sound, bell or terminal", v)
			}
		}
	}
	return n, nil
}

// FileNotify reads the notifications section. A file that is absent or
// does not parse gives DefaultNotify; Load reports the bad key.
func FileNotify(cfgData []byte) Notify {
	var f File
	if len(cfgData) == 0 || yaml.Unmarshal(cfgData, &f) != nil {
		return DefaultNotify()
	}
	n, err := f.Notifications.parse()
	if err != nil {
		return DefaultNotify()
	}
	return n
}
