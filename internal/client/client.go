// Package client talks to the greyman daemon's HTTP/JSON API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// idleTimeout is how long the daemon may stay silent before Do gives up. The daemon
// writes a keepalive every 10s while a command runs.
const idleTimeout = time.Minute

type Client struct {
	base  string
	token string
	http  *http.Client
	idle  time.Duration
}

// ErrUnreachable marks transport failures: nothing listening, connection dropped, or
// the daemon went silent. It is distinct from an APIError, where the daemon answered.
var ErrUnreachable = errors.New("daemon unreachable")

// Response is the daemon's JSON envelope. PNG is base64 on the wire; encoding/json
// decodes it into raw bytes.
type Response struct {
	OK    bool   `json:"ok"`
	Text  string `json:"text"`
	PNG   []byte `json:"png"`
	Error string `json:"error"`
}

// APIError is an error reported by the daemon (as opposed to a transport failure).
type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string { return e.Msg }

func New(port int, token string) *Client {
	return &Client{
		base:  "http://127.0.0.1:" + strconv.Itoa(port),
		token: token,
		// No overall timeout: actions can sit in the rate limiter or type for minutes.
		// Do enforces idle silence instead.
		http: &http.Client{},
		idle: idleTimeout,
	}
}

// idleReader pushes the watchdog back each time bytes arrive.
type idleReader struct {
	r io.Reader
	t *time.Timer
	d time.Duration
}

func (i idleReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	if n > 0 {
		i.t.Reset(i.d)
	}
	return n, err
}

// Do calls /v1/<name>. "status" is a GET; everything else POSTs args as JSON.
func (c *Client) Do(ctx context.Context, name string, args map[string]any) (*Response, error) {
	method, body := http.MethodGet, io.Reader(nil)
	if name != "status" {
		b, err := json.Marshal(args)
		if err != nil {
			return nil, err
		}
		method, body = http.MethodPost, bytes.NewReader(b)
	}

	parent := ctx
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var silent atomic.Bool
	watchdog := time.AfterFunc(c.idle, func() { silent.Store(true); cancel() })
	defer watchdog.Stop()
	unreachable := func(err error) error {
		if perr := parent.Err(); perr != nil {
			return perr // the caller gave up (Ctrl-C); the daemon sees the hang-up and stops
		}
		if silent.Load() {
			err = fmt.Errorf("no data for %s", c.idle)
		}
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+"/v1/"+name, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return nil, unreachable(err)
	}
	defer res.Body.Close()
	watchdog.Reset(c.idle)

	var r Response
	if err := json.NewDecoder(io.LimitReader(idleReader{res.Body, watchdog, c.idle}, 64<<20)).Decode(&r); err != nil {
		if silent.Load() || parent.Err() != nil {
			return nil, unreachable(err)
		}
		return nil, &APIError{Status: res.StatusCode, Msg: fmt.Sprintf("bad response (HTTP %d)", res.StatusCode)}
	}
	if !r.OK || res.StatusCode != http.StatusOK {
		msg := r.Error
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", res.StatusCode)
		}
		return nil, &APIError{Status: res.StatusCode, Msg: msg}
	}
	return &r, nil
}
