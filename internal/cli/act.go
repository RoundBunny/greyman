package cli

import (
	"context"
	"flag"
	"fmt"
	"strconv"
)

// actFlags are shared by every command that changes page state.
type actFlags struct {
	tab         string
	snap        bool
	wait        string
	waitTimeout int
}

func (a *actFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&a.tab, "tab", "", "tab id (default: active tab)")
	fs.BoolVar(&a.snap, "snap", false, "append a fresh snapshot to the output")
	bindWait(fs, &a.wait, &a.waitTimeout)
}

// waitUsage is the wait options in the usage line of snap, open and every action.
const waitUsage = "[--wait W] [--wait-timeout MS]"

// bindWait registers --wait and --wait-timeout, shared by snap and every action.
func bindWait(fs *flag.FlagSet, wait *string, timeout *int) {
	fs.StringVar(wait, "wait", "", "wait first/after: idle (default), text=TEXT, css=SELECTOR or none (a timeout is reported, not an error)")
	fs.IntVar(timeout, "wait-timeout", 0, "max milliseconds for --wait (default: the DEFAULT_WAIT setting, 3000)")
}

// waitArgs adds the wait options to a request body when they were given.
func waitArgs(body map[string]any, wait string, timeout int) {
	if wait != "" {
		body["wait"] = wait
	}
	if timeout != 0 {
		body["waitTimeout"] = timeout
	}
}

// runAction sends an action, prints its one-line status, and optionally a snapshot.
func runAction(ctx context.Context, e *Env, name string, a actFlags, args map[string]any) error {
	if a.tab != "" {
		args["tab"] = a.tab
	}
	waitArgs(args, a.wait, a.waitTimeout)
	resp, err := call(ctx, name, args)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Out, resp.Text)
	if a.snap {
		return printSnap(ctx, e, a.tab, 0)
	}
	return nil
}

func cmdClick(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman click <ref> | --name TEXT [--role R] [--exact] [--nth N] [--tab tN] [--snap] " + waitUsage
	fs, a := newFlags("click", e, usage), actFlags{}
	a.bind(fs)
	name := fs.String("name", "", "click the element with this accessible name instead of a ref")
	role := fs.String("role", "", "with --name: only this role (button, link, ...)")
	exact := fs.Bool("exact", false, "with --name: match the whole name, case-sensitive")
	nth := fs.Int("nth", 0, "with --name: pick the Nth match (1-based) when several match")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	byName := *name != ""
	if len(pos) > 1 || (len(pos) == 1) == byName || (!byName && (*role != "" || *exact || *nth != 0)) {
		return usageError{usage}
	}
	body := map[string]any{}
	if byName {
		body["name"] = *name
		if *role != "" {
			body["role"] = *role
		}
		if *exact {
			body["exact"] = true
		}
		if *nth != 0 {
			body["nth"] = *nth
		}
	} else {
		body["ref"] = pos[0]
	}
	return runAction(ctx, e, "click", a, body)
}

func cmdType(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman type <ref> <text> [--submit] [--tab tN] [--snap] " + waitUsage
	fs, a := newFlags("type", e, usage), actFlags{}
	a.bind(fs)
	submit := fs.Bool("submit", false, "press Enter after typing")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 2 {
		return usageError{usage}
	}
	return runAction(ctx, e, "type", a, map[string]any{"ref": pos[0], "text": pos[1], "submit": *submit})
}

func cmdScroll(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman scroll <up|down> [n] [--tab tN] [--snap] " + waitUsage
	fs, a := newFlags("scroll", e, usage), actFlags{}
	a.bind(fs)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) < 1 || len(pos) > 2 || (pos[0] != "up" && pos[0] != "down") {
		return usageError{usage}
	}
	units := 1
	if len(pos) == 2 {
		if units, err = strconv.Atoi(pos[1]); err != nil || units < 1 {
			return usageError{usage}
		}
	}
	return runAction(ctx, e, "scroll", a, map[string]any{"direction": pos[0], "units": units})
}
