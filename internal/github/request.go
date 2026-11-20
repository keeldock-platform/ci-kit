package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/keeldock-platform/ci-kit/internal/config"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Request never retries a write. Read retry budget is shared by its two-minute context.
func (c *Client) Request(ctx context.Context, method, path string, body any, out any) (int, error) {
	if path == "" || path[0] == '/' {
		return 0, Fail(3, "policy_refused")
	}
	var payload []byte
	var e error
	if body != nil {
		payload, e = json.Marshal(body)
		if e != nil || len(payload) > 8192 {
			return 0, Fail(2, "usage")
		}
	}
	if method == http.MethodGet {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
	}
	for attempt := 0; ; attempt++ {
		reqctx, cancel := context.WithTimeout(ctx, c.Timeout)
		resp, err := c.REST.RequestWithContext(reqctx, method, path, bytes.NewReader(payload))
		if err == nil {
			b, e := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
			resp.Body.Close()
			cancel()
			if e != nil || len(b) > 8<<20 {
				return 0, Fail(6, "missing_evidence")
			}
			if len(b) > 0 && config.ValidateJSON(b, 8<<20) != nil {
				return resp.StatusCode, Fail(6, "missing_evidence")
			}
			if len(b) > 0 && out != nil {
				if e = json.Unmarshal(b, out); e != nil {
					return resp.StatusCode, Fail(6, "missing_evidence")
				}
			}
			return resp.StatusCode, nil
		}
		cancel()
		if ctx.Err() != nil {
			return 0, Fail(4, "deadline")
		}
		if method != http.MethodGet || attempt >= 2 {
			return 0, classify(err)
		}
		retry := false
		delay := []time.Duration{10 * time.Second, 30 * time.Second}[attempt]
		var he *api.HTTPError
		if errors.As(err, &he) {
			switch he.StatusCode {
			case 429, 502, 503, 504:
				retry = true
			case 403:
				retry = he.Headers.Get("X-RateLimit-Remaining") == "0"
			}
			if s := he.Headers.Get("Retry-After"); s != "" {
				if n, e := strconv.Atoi(s); e == nil && n >= 0 {
					delay = time.Duration(n) * time.Second
				} else if at, e := http.ParseTime(s); e == nil {
					delay = time.Until(at)
				}
			}
			if he.Headers.Get("X-RateLimit-Remaining") == "0" {
				if n, e := strconv.ParseInt(he.Headers.Get("X-RateLimit-Reset"), 10, 64); e == nil {
					if d := time.Until(time.Unix(n, 0)); d > delay {
						delay = d
					}
				}
			}
		} else {
			var ne net.Error
			retry = errors.As(err, &ne)
		}
		if !retry {
			return 0, classify(err)
		}
		if delay < 0 {
			delay = 0
		}
		if deadline, ok := ctx.Deadline(); ok && time.Now().Add(delay).After(deadline) {
			return 0, Fail(4, "deadline")
		}
		if e = c.Sleep(ctx, delay); e != nil {
			return 0, Fail(4, "deadline")
		}
	}
}
func classify(e error) error {
	var f *Failure
	if errors.As(e, &f) {
		return f
	}
	var he *api.HTTPError
	if errors.As(e, &he) {
		if he.StatusCode == 401 || he.StatusCode == 403 || he.StatusCode == 404 || he.StatusCode == 422 {
			return Fail(3, "auth_refused")
		}
	}
	return Fail(6, "missing_evidence")
}
