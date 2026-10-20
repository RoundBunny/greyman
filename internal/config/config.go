// Package config locates the greyman config directory and reads and writes
// options.cfg, a file of KEY=VALUE lines. Blank lines and lines starting with # are
// ignored.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"greyman/internal/atomic"
)

const (
	fileName = "options.cfg"
	header   = "# greyman options: KEY=VALUE, one per line"
)

// Dir returns the directory holding options.cfg and state.json. GREYMAN_CONFIG_DIR
// overrides the default (used by tests).
func Dir() (string, error) {
	if dir := os.Getenv("GREYMAN_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "greyman"), nil
}

func path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// readLines returns the file's lines; a missing or empty file has none.
func readLines(p string) ([]string, error) {
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return nil, nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"), nil
}

func parseLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	key, value, ok = strings.Cut(line, "=")
	return strings.TrimSpace(key), strings.TrimSpace(value), ok
}

// Load returns every key in the file. A missing file is an empty map. If a key
// repeats, the first line wins (the one Set rewrites).
func Load() (map[string]string, error) {
	p, err := path()
	if err != nil {
		return nil, err
	}
	lines, err := readLines(p)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, l := range lines {
		if k, v, ok := parseLine(l); ok {
			if _, dup := m[k]; !dup {
				m[k] = v
			}
		}
	}
	return m, nil
}

// Get returns the value for key, or "" if it is not set.
func Get(key string) (string, error) {
	m, err := Load()
	return m[key], err
}

// Set rewrites key's line in place, or appends it, keeping comments and other lines.
// The file is written 0600.
func Set(key, value string) error {
	return Update(key, func(string) string { return value })
}

// Update replaces key's value with fn(current value), where the current value is ""
// if the key is not set. The read-modify-write is serialized across processes, so
// concurrent callers cannot lose each other's changes.
func Update(key string, fn func(old string) string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	return modify(func(lines []string) ([]string, bool, error) {
		if lines == nil {
			lines = []string{header}
		}
		idx, old := -1, ""
		for i, l := range lines {
			if k, v, ok := parseLine(l); ok && k == key {
				idx, old = i, v
				break
			}
		}
		value := fn(old)
		if strings.Contains(value, "\n") {
			return nil, false, fmt.Errorf("invalid config value for %q: contains a newline", key)
		}
		entry := key + "=" + value
		if idx >= 0 {
			lines[idx] = entry
		} else {
			lines = append(lines, entry)
		}
		return lines, true, nil
	})
}

// Delete removes every line that sets key, keeping comments and other lines. A key
// that is not set is not an error and leaves the file untouched.
func Delete(key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	return modify(func(lines []string) ([]string, bool, error) {
		kept := make([]string, 0, len(lines))
		for _, l := range lines {
			if k, _, ok := parseLine(l); !ok || k != key {
				kept = append(kept, l)
			}
		}
		return kept, len(kept) != len(lines), nil
	})
}

func checkKey(key string) error {
	if key == "" || strings.ContainsAny(key, "=\n") {
		return fmt.Errorf("invalid config key %q", key)
	}
	return nil
}

// modify runs fn on the file's lines (nil if the file is missing or empty) while
// holding the cross-process lock, and writes back what it returns when write is true.
func modify(fn func(lines []string) (out []string, write bool, err error)) error {
	p, err := path()
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	unlock, err := lockDir(dir)
	if err != nil {
		return err
	}
	defer unlock()

	lines, err := readLines(p)
	if err != nil {
		return err
	}
	out, write, err := fn(lines)
	if err != nil || !write {
		return err
	}
	var content string // an empty file, not a lone newline, so the next Update re-adds the header
	if len(out) > 0 {
		content = strings.Join(out, "\n") + "\n"
	}
	// 0600: options.cfg can hold a proxy URL with credentials.
	return atomic.WriteFile(p, []byte(content), 0o600)
}
