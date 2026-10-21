package cli

import "context"

func cmdLog(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman log [--errors] [--max N] [--clear] [--tab tN]"
	fs := newFlags("log", e, usage)
	tab := fs.String("tab", "", "tab id (default: active tab)")
	max := fs.Int("max", 0, "show the last N entries (default 50)")
	errorsOnly := fs.Bool("errors", false, "hide warnings and navigation markers")
	clear := fs.Bool("clear", false, "forget the entries after showing them")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 || *max < 0 {
		return usageError{usage}
	}
	body := viewBody(*tab, *max)
	if *errorsOnly {
		body["errors"] = true
	}
	if *clear {
		body["clear"] = true
	}
	return textView(ctx, e, "log", body)
}
