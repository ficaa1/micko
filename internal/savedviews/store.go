// Package savedviews stores named workflow scopes without credentials or resources.
package savedviews

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ficaa1/micko/internal/journal"
)

type View struct {
	Profile       string `json:"profile"`
	Namespace     string `json:"namespace"`
	AllNamespaces bool   `json:"allNamespaces"`
	Query         string `json:"query"`
	Phase         string `json:"phase"`
	Sort          string `json:"sort"`
}

type Store struct {
	path string
	err  error
}

type file struct {
	Version int             `json:"version"`
	Views   map[string]View `json:"views"`
}

// Open keeps views.json beside the action journal.
func Open() *Store {
	path, err := journal.DefaultPath()
	if err != nil {
		return &Store{err: errors.New("no saved views state directory")}
	}
	return &Store{path: filepath.Join(filepath.Dir(path), "views.json")}
}

func ValidName(name string) bool {
	if name == "" || len([]rune(name)) > 80 || name != strings.TrimSpace(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func (s *Store) Read() (map[string]View, error) {
	if s.err != nil {
		return nil, s.err
	}
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]View{}, nil
	}
	if err != nil {
		return nil, errors.New("cannot read saved views file")
	}
	defer f.Close()
	var data file
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	if err := d.Decode(&data); err != nil {
		return nil, errors.New("saved views file is corrupt; keep it for recovery")
	}
	var extra any
	if d.Decode(&extra) != io.EOF || data.Version != 1 || data.Views == nil {
		return nil, errors.New("saved views file has an unsupported format; keep it for recovery")
	}
	for name := range data.Views {
		if !ValidName(name) {
			return nil, errors.New("saved views file contains an invalid name")
		}
	}
	return data.Views, nil
}

// Change rereads under an exclusive lock and replaces the file atomically.
func (s *Store) Change(name string, view *View) error {
	if !ValidName(name) {
		return errors.New("use a view name of 1–80 printable characters")
	}
	if s.err != nil {
		return s.err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return errors.New("cannot create saved views directory")
	}
	lock := s.path + ".lock"
	if err := os.Mkdir(lock, 0700); err != nil {
		return errors.New("saved views are busy or locked (remove " + lock + " if no micko is running); no state changed")
	}
	defer os.Remove(lock)
	views, err := s.Read()
	if err != nil {
		return err
	}
	if view == nil {
		if _, ok := views[name]; !ok {
			return errors.New("no saved view with that name")
		}
		delete(views, name)
	} else {
		views[name] = *view
	}
	data, err := json.MarshalIndent(file{1, views}, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".views-*")
	if err != nil {
		return errors.New("cannot write saved views; no state changed")
	}
	defer os.Remove(f.Name())
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), s.path)
	}
	if err != nil {
		return errors.New("cannot replace saved views; no state changed")
	}
	return nil
}
