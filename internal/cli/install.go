package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"greyman/internal/agents"
	"greyman/internal/config"
	"greyman/internal/version"
)

const (
	keyAgents  = "INSTALLED_AGENTS"
	keyVersion = "INSTALLED_VERSION"
)

// hostFunc is a seam for tests, which point detection at a temp home.
var hostFunc = agents.DefaultHost

func cmdInstall(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman install [claude|codex|cursor|copilot|all|other [dir]] [--force]"
	fs := newFlags("install", e, usage)
	force := fs.Bool("force", false, "replace an existing Codex rules file")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	host, err := hostFunc()
	if err != nil {
		return err
	}

	dir := "."
	var picked []agents.Agent
	switch {
	case len(pos) == 0:
		if picked = agents.Detected(host); len(picked) == 0 {
			return fmt.Errorf("no supported agent detected\nsupported: %s\nrun `greyman install all` or `greyman install other [dir]`", knownNames())
		}
	case len(pos) == 1 && pos[0] == "all":
		picked = agents.Known
	case pos[0] == "other" && len(pos) <= 2:
		picked = []agents.Agent{agents.Other}
		if len(pos) == 2 {
			dir = pos[1]
		}
	case len(pos) == 1:
		a, ok := agents.Lookup(pos[0])
		if !ok {
			fmt.Fprintf(e.Err, "unknown agent %q; supported: %s, all, other\n", pos[0], knownNames())
			return usageError{usage}
		}
		picked = []agents.Agent{a}
	default:
		return usageError{usage}
	}

	// Agents share files (~/.agents/skills), so each path is written once.
	type outcome struct {
		kept bool
		err  error
	}
	written := map[string]outcome{}
	var done []string
	failed := 0
	for _, a := range picked {
		var parts []string
		ok := true
		for _, t := range a.Targets(host, dir) {
			o, seen := written[t.Path]
			if !seen {
				o.kept, o.err = agents.Write(t, *force)
				written[t.Path] = o
			}
			switch {
			case o.err != nil:
				fmt.Fprintf(e.Err, "fail %s %s=%s: %v\n", a.Name, t.Label, t.Path, o.err)
				ok = false
			case o.kept:
				parts = append(parts, fmt.Sprintf("kept existing %s=%s", t.Label, t.Path))
			default:
				parts = append(parts, fmt.Sprintf("%s=%s", t.Label, t.Path))
			}
		}
		if !ok {
			failed++
			continue
		}
		fmt.Fprintf(e.Out, "ok %s %s\n", a.Name, strings.Join(parts, " "))
		done = append(done, a.Name)
	}

	if len(done) > 0 {
		installed, err := recordInstall(done)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.Out, "ok installed=%s\n", installed)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d agents failed", failed, len(picked))
	}
	return nil
}

func knownNames() string {
	names := make([]string, len(agents.Known))
	for i, a := range agents.Known {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

// splitAgents parses a comma-separated INSTALLED_AGENTS value.
func splitAgents(v string) []string {
	var names []string
	for _, n := range strings.Split(v, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// installedAgents returns the recorded agents, sorted, or nil if none.
func installedAgents() ([]string, error) {
	v, err := config.Get(keyAgents)
	return splitAgents(v), err
}

// recordInstall unions names into INSTALLED_AGENTS (never removing any) and records
// the binary's version. It returns the new comma-separated agent list.
func recordInstall(names []string) (string, error) {
	var list string
	err := config.Update(keyAgents, func(old string) string {
		all := slices.Compact(slices.Sorted(slices.Values(append(splitAgents(old), names...))))
		list = strings.Join(all, ",")
		return list
	})
	if err != nil {
		return "", err
	}
	return list, config.Set(keyVersion, version.Version)
}

// requireSetup is the gate every command except the ungated ones passes through.
func requireSetup() error {
	names, err := installedAgents()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errors.New("greyman is not set up for any agent; run `greyman install`")
	}
	return nil
}
