// Package state persists what the CLI needs to find the running container.
package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"greyman/internal/atomic"
	"greyman/internal/config"
)

type State struct {
	Container string `json:"container"`
	Port      int    `json:"port"`
	WatchPort int    `json:"watch_port,omitempty"` // noVNC live view; 0 when not enabled
	Token     string `json:"token"`
}

var ErrNotRunning = errors.New("greyman is not running; run `greyman up`")

// path returns the state file location, beside options.cfg.
func path() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

func Load() (*State, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotRunning
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Save writes the state file (0600: it holds the daemon's bearer token).
func Save(s *State) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return atomic.WriteFile(p, b, 0o600)
}

func Remove() error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
