package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// textView sends a read-only command whose response text is the whole output.
func textView(ctx context.Context, e *Env, name string, tab string, max int) error {
	body := map[string]any{}
	if tab != "" {
		body["tab"] = tab
	}
	if max > 0 {
		body["max"] = max
	}
	resp, err := call(ctx, name, body)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Out, resp.Text)
	return nil
}

func printSnap(ctx context.Context, e *Env, tab string, max int) error {
	return textView(ctx, e, "snap", tab, max)
}

func cmdSnap(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman snap [--tab tN] [--max N]"
	fs := newFlags("snap", e, usage)
	tab := fs.String("tab", "", "tab id (default: active tab)")
	max := fs.Int("max", 0, "max characters (default: daemon setting)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageError{usage}
	}
	return printSnap(ctx, e, *tab, *max)
}

func cmdRead(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman read [--tab tN] [--max N]"
	fs := newFlags("read", e, usage)
	tab := fs.String("tab", "", "tab id (default: active tab)")
	max := fs.Int("max", 0, "max characters (default: daemon setting)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageError{usage}
	}
	return textView(ctx, e, "read", *tab, *max)
}

func cmdShot(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman shot [ref] [-o path] [--full] [--tab tN]"
	fs := newFlags("shot", e, usage)
	tab := fs.String("tab", "", "tab id (default: active tab)")
	out := fs.String("o", "", "output PNG path (default: a temp file)")
	full := fs.Bool("full", false, "capture the full scrollable page")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || (len(pos) == 1 && *full) {
		return usageError{usage}
	}
	body := map[string]any{"full": *full}
	if *tab != "" {
		body["tab"] = *tab
	}
	if len(pos) == 1 {
		body["ref"] = pos[0]
	}
	resp, err := call(ctx, "shot", body)
	if err != nil {
		return err
	}
	path := *out
	if path == "" {
		path = filepath.Join(os.TempDir(), fmt.Sprintf("greyman-%d.png", time.Now().UnixNano()))
	}
	if err := os.WriteFile(path, resp.PNG, 0o644); err != nil {
		return err
	}
	fmt.Fprintln(e.Out, path)
	return nil
}
