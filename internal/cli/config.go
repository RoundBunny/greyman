package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"greyman/internal/config"
	"greyman/internal/docker"
	"greyman/internal/settings"
	"greyman/internal/state"
)

func cmdConfig(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman config [list | get KEY | set KEY VALUE | unset KEY]"
	fs := newFlags("config", e, usage)
	pos, err := parseNumbers(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		pos = []string{"list"}
	}
	verb, rest := pos[0], pos[1:]
	switch {
	case verb == "list" && len(rest) == 0:
		return configList(e)
	case verb == "get" && len(rest) == 1:
		return configGet(e, rest[0])
	case verb == "set" && len(rest) == 2:
		return configSet(ctx, e, rest[0], rest[1])
	case verb == "unset" && len(rest) == 1:
		return configUnset(ctx, e, rest[0])
	}
	return usageError{usage}
}

func configList(e *Env) error {
	entries, err := settings.Inspect()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(e.Out, 0, 0, 2, ' ', 0)
	for _, en := range entries {
		note := en.Setting.Doc
		if en.Err != nil {
			note = "INVALID: " + errors.Unwrap(en.Err).Error()
		}
		fmt.Fprintf(w, "%s=%s\t[%s]\t%s\n", en.Setting.Key, settings.Display(en.Setting.Key, en.Value.Val), en.Value.Source, note)
	}
	return w.Flush()
}

func configGet(e *Env, key string) error {
	if _, err := knownSetting(key); err != nil {
		return err
	}
	entries, err := settings.Inspect()
	if err != nil {
		return err
	}
	for _, en := range entries {
		if en.Setting.Key == key {
			if en.Err != nil {
				return en.Err
			}
			fmt.Fprintln(e.Out, settings.Display(key, en.Value.Val))
		}
	}
	return nil
}

func configSet(ctx context.Context, e *Env, key, value string) error {
	s, err := knownSetting(key)
	if err != nil {
		return err
	}
	if value == "" {
		return fmt.Errorf("empty value; use `greyman config unset %s` to remove it", key)
	}
	if key == "PROXY" && strings.Contains(value, "@") {
		dir, _ := config.Dir()
		return fmt.Errorf("proxy credentials would show up in `ps` and shell history; add a PROXY=... line to %s/options.cfg yourself", dir)
	}
	if err := s.Validate(value); err != nil {
		return fmt.Errorf("invalid %s: %w", key, err)
	}
	if err := config.Set(key, value); err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "ok %s=%s\n", key, settings.Display(key, value))
	noteIfRunning(ctx, e)
	return nil
}

func configUnset(ctx context.Context, e *Env, key string) error {
	if _, err := knownSetting(key); err != nil {
		return err
	}
	if err := config.Delete(key); err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "ok unset %s\n", key)
	noteIfRunning(ctx, e)
	return nil
}

func knownSetting(key string) (settings.Setting, error) {
	s, ok := settings.Lookup(key)
	if !ok {
		return s, fmt.Errorf("unknown setting %q; run `greyman config list`", key)
	}
	return s, nil
}

// noteIfRunning tells the user a change has not reached a container that is already up.
func noteIfRunning(ctx context.Context, e *Env) {
	if st, err := state.Load(); err == nil && docker.Running(ctx, st.Container) {
		fmt.Fprintln(e.Out, "note: the container is running; changes apply after `greyman down` and `greyman up`")
	}
}
