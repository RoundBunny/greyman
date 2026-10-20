package cli

import (
	"context"
	"flag"
	"fmt"
	"strconv"
)

// actFlags are shared by every command that changes page state.
type actFlags struct {
	tab  string
	snap bool
}

func (a *actFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&a.tab, "tab", "", "tab id (default: active tab)")
	fs.BoolVar(&a.snap, "snap", false, "append a fresh snapshot to the output")
}

// runAction sends an action, prints its one-line status, and optionally a snapshot.
func runAction(ctx context.Context, e *Env, name string, a actFlags, args map[string]any) error {
	if a.tab != "" {
		args["tab"] = a.tab
	}
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
	const usage = "greyman click <ref> [--tab tN] [--snap]"
	fs, a := newFlags("click", e, usage), actFlags{}
	a.bind(fs)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageError{usage}
	}
	return runAction(ctx, e, "click", a, map[string]any{"ref": pos[0]})
}

func cmdType(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman type <ref> <text> [--submit] [--tab tN] [--snap]"
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
	const usage = "greyman scroll <up|down> [n] [--tab tN] [--snap]"
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
