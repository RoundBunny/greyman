package cli

import (
	"context"
	"fmt"
)

func cmdOpen(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman open <url> [--tab tN] [--snap]"
	fs, a := newFlags("open", e, usage), actFlags{}
	a.bind(fs)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usageError{usage}
	}
	return runAction(ctx, e, "open", a, map[string]any{"url": pos[0]})
}

func cmdBack(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman back [--tab tN] [--snap]"
	fs, a := newFlags("back", e, usage), actFlags{}
	a.bind(fs)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageError{usage}
	}
	return runAction(ctx, e, "back", a, map[string]any{})
}

func cmdClose(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman close [--tab tN]"
	fs := newFlags("close", e, usage)
	tab := fs.String("tab", "", "tab id (default: active tab)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageError{usage}
	}
	body := map[string]any{}
	if *tab != "" {
		body["tab"] = *tab
	}
	resp, err := call(ctx, "close", body)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Out, resp.Text)
	return nil
}
