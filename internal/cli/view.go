package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// textView sends a read-only command whose response text is the whole output.
func textView(ctx context.Context, e *Env, name string, body map[string]any) error {
	resp, err := call(ctx, name, body)
	if err != nil {
		return err
	}
	fmt.Fprintln(e.Out, resp.Text)
	return nil
}

// viewBody is the request body shared by the read-only views.
func viewBody(tab string, max int) map[string]any {
	body := map[string]any{}
	if tab != "" {
		body["tab"] = tab
	}
	if max > 0 {
		body["max"] = max
	}
	return body
}

func printSnap(ctx context.Context, e *Env, tab string, max int) error {
	return textView(ctx, e, "snap", viewBody(tab, max))
}

func cmdSnap(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman snap [--modal] [--grep RE] [--role R] [--within REF|RE] [--max N] [--wait W] [--tab tN]"
	fs := newFlags("snap", e, usage)
	tab := fs.String("tab", "", "tab id (default: active tab)")
	max := fs.Int("max", 0, "max characters (default: daemon setting)")
	modal := fs.Bool("modal", false, "dialogs only")
	grep := fs.String("grep", "", "only lines matching this pattern (case-insensitive), plus their ancestors")
	role := fs.String("role", "", "only nodes with this role (comma-separated), plus their ancestors")
	within := fs.String("within", "", "only the subtree of this ref, or of lines matching this pattern")
	var wait string
	var waitTimeout int
	bindWait(fs, &wait, &waitTimeout)
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usageError{usage}
	}
	body := viewBody(*tab, *max)
	if *modal {
		body["modal"] = true
	}
	for k, v := range map[string]string{"grep": *grep, "role": *role, "within": *within} {
		if v != "" {
			body[k] = v
		}
	}
	waitArgs(body, wait, waitTimeout)
	return textView(ctx, e, "snap", body)
}

func cmdRead(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman read [ref] [--modal] [--tab tN] [--max N]"
	fs := newFlags("read", e, usage)
	tab := fs.String("tab", "", "tab id (default: active tab)")
	max := fs.Int("max", 0, "max characters (default: daemon setting)")
	modal := fs.Bool("modal", false, "read the open dialogs instead of the page")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || (len(pos) == 1 && *modal) {
		return usageError{usage}
	}
	body := viewBody(*tab, *max)
	if *modal {
		body["modal"] = true
	}
	if len(pos) == 1 {
		body["ref"] = pos[0]
	}
	return textView(ctx, e, "read", body)
}

func cmdShot(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman shot [ref] [--modal] [--full] [--max-width N] [-o path] [--tab tN]"
	fs := newFlags("shot", e, usage)
	tab := fs.String("tab", "", "tab id (default: active tab)")
	out := fs.String("o", "", "output PNG path (default: a temp file)")
	full := fs.Bool("full", false, "capture the full scrollable page")
	modal := fs.Bool("modal", false, "capture the largest open dialog")
	maxWidth := fs.Int("max-width", 0, "scale the image down to this many pixels wide (fewer image tokens)")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 || (len(pos) == 1 && (*full || *modal)) || (*full && *modal) || *maxWidth < 0 {
		return usageError{usage}
	}
	body := map[string]any{"full": *full}
	if *modal {
		body["modal"] = true
	}
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
	data := resp.PNG
	if *maxWidth > 0 {
		if data, err = downscale(data, *maxWidth); err != nil {
			return err
		}
	}
	path := *out
	if path == "" {
		path = filepath.Join(os.TempDir(), fmt.Sprintf("greyman-%d.png", time.Now().UnixNano()))
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintln(e.Out, path)
	return nil
}

// imageExt maps the image types a site serves to a file extension.
var imageExt = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp",
	"image/gif": ".gif", "image/avif": ".avif", "image/svg+xml": ".svg",
}

func cmdSave(ctx context.Context, e *Env, args []string) error {
	const usage = "greyman save <ref> [-o path] [--url] [--tab tN]"
	fs := newFlags("save", e, usage)
	tab := fs.String("tab", "", "tab id (default: active tab)")
	out := fs.String("o", "", "output path (default: a temp file with the image's extension)")
	urlOnly := fs.Bool("url", false, "print the image URL instead of downloading it")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || (*urlOnly && *out != "") {
		return usageError{usage}
	}
	body := map[string]any{"ref": pos[0]}
	if *tab != "" {
		body["tab"] = *tab
	}
	if *urlOnly {
		body["url"] = true
	}
	resp, err := call(ctx, "save", body)
	if err != nil {
		return err
	}
	if *urlOnly {
		fmt.Fprintln(e.Out, resp.Text)
		return nil
	}
	path := *out
	if path == "" {
		ext := imageExt[resp.ContentType]
		if ext == "" {
			ext = ".img"
		}
		path = filepath.Join(os.TempDir(), fmt.Sprintf("greyman-%d%s", time.Now().UnixNano(), ext))
	}
	if err := os.WriteFile(path, resp.File, 0o644); err != nil {
		return err
	}
	fmt.Fprintln(e.Out, path)
	return nil
}
