package client

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return New(port, "secret")
}

func TestDoPostsJSONWithBearer(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/open" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("bad auth header %q", r.Header.Get("Authorization"))
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["url"] != "example.com" {
			t.Errorf("body = %v", in)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "text": "ok url=https://example.com/"})
	})
	r, err := c.Do(context.Background(), "open", map[string]any{"url": "example.com"})
	if err != nil || r.Text != "ok url=https://example.com/" {
		t.Fatalf("Do = %+v, %v", r, err)
	}
}

func TestStatusIsGET(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("status used %s", r.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "text": "ok"})
	})
	if _, err := c.Do(context.Background(), "status", nil); err != nil {
		t.Fatal(err)
	}
}

func TestDecodesPNG(t *testing.T) {
	raw := []byte{0x89, 'P', 'N', 'G'}
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "text": "ok", "png": base64.StdEncoding.EncodeToString(raw)})
	})
	r, err := c.Do(context.Background(), "shot", map[string]any{})
	if err != nil || string(r.PNG) != string(raw) {
		t.Fatalf("PNG = %v, %v", r, err)
	}
}

func TestAPIError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "ref e9 not found; run snap"})
	})
	_, err := c.Do(context.Background(), "click", map[string]any{"ref": "e9"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 400 || apiErr.Msg != "ref e9 not found; run snap" {
		t.Fatalf("err = %#v", err)
	}
}

func TestUnreachableIsNotAPIError(t *testing.T) {
	c := New(1, "x") // nothing listens on port 1
	_, err := c.Do(context.Background(), "status", nil)
	var apiErr *APIError
	if err == nil || errors.As(err, &apiErr) || !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable and not an APIError", err)
	}
}

// Keepalives (leading JSON whitespace) hold a slow command open past the idle timeout.
func TestKeepalivesOutlastTheIdleTimeout(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		for range 8 {
			_, _ = w.Write([]byte(" "))
			w.(http.Flusher).Flush()
			time.Sleep(40 * time.Millisecond)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "text": "typed"})
	})
	c.idle = 150 * time.Millisecond // total run is ~320ms
	r, err := c.Do(context.Background(), "type", map[string]any{})
	if err != nil || r.Text != "typed" {
		t.Fatalf("Do = %+v, %v", r, err)
	}
}

func TestKeepaliveFailureArrivesInBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(" "))
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "ref e9 not found"})
	})
	_, err := c.Do(context.Background(), "click", map[string]any{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Msg != "ref e9 not found" {
		t.Fatalf("err = %#v", err)
	}
}

func TestSilenceIsUnreachable(t *testing.T) {
	// The server only notices a client hang-up once the request body has been read.
	stall := func(r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}
	for name, h := range map[string]http.HandlerFunc{
		"before headers": func(_ http.ResponseWriter, r *http.Request) { stall(r) },
		"mid body": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(" "))
			w.(http.Flusher).Flush()
			stall(r)
		},
	} {
		c := newTestClient(t, h)
		c.idle = 80 * time.Millisecond
		start := time.Now()
		_, err := c.Do(context.Background(), "snap", map[string]any{})
		var apiErr *APIError
		if !errors.Is(err, ErrUnreachable) || errors.As(err, &apiErr) || time.Since(start) > 2*time.Second {
			t.Errorf("%s: err = %v after %v", name, err, time.Since(start))
		}
	}
}

// Ctrl-C: the caller's context ends the call, and the daemon sees the connection close.
func TestCancelledContextIsNotUnreachable(t *testing.T) {
	hungUp := make(chan struct{})
	c := newTestClient(t, func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // the server notices a hang-up once the body is read
		<-r.Context().Done()
		close(hungUp)
	})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := c.Do(ctx, "type", map[string]any{})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want context.Canceled and not ErrUnreachable", err)
	}
	select {
	case <-hungUp:
	case <-time.After(2 * time.Second):
		t.Fatal("server never saw the connection close")
	}
}

func TestDoReportsTheDaemonVersionAndFileFields(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Greyman-Version", "v9")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "file": base64.StdEncoding.EncodeToString([]byte("img")), "content_type": "image/png"})
	})
	r, err := c.Do(context.Background(), "save", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "v9" || string(r.File) != "img" || r.ContentType != "image/png" {
		t.Fatalf("response = %+v", r)
	}
}

func TestDoLeavesVersionEmptyForAnOlderDaemon(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "text": "ok"})
	})
	r, err := c.Do(context.Background(), "status", nil)
	if err != nil || r.Version != "" {
		t.Fatalf("response = %+v, err %v", r, err)
	}
}
