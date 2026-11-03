// Package faceit is a client of the FACEIT Data API: players, their match
// history, match details and match stats. It knows nothing of Telegram or the
// database. See docs/notes/faceit-data-api.md.
package faceit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the Data API v4.
const DefaultBaseURL = "https://open.faceit.com/data/v4"

const (
	// requestTimeout bounds one attempt of a request.
	requestTimeout = 10 * time.Second
	// maxAttempts is how many times a request is tried: network errors, 429
	// and 5xx are retried, other errors are not.
	maxAttempts = 3
	// retryWait is the pause before the second attempt; it doubles for the
	// next one.
	retryWait = 500 * time.Millisecond
	// maxRetryWait caps the pause FACEIT asks for with Retry-After or
	// RateLimit-Reset.
	maxRetryWait = 5 * time.Second
	// maxBodySize bounds a response body. The largest one we read, match
	// stats, is about 25 KB.
	maxBodySize = 2 << 20

	// FACEIT allows 20 requests per second (RateLimit-Limit: 20, 20;w=1).
	// We stay at a quarter of that, with bursts of up to 10 for a /matches.
	rateInterval = 200 * time.Millisecond
	rateBurst    = 10
)

// ErrNotFound means FACEIT has no such player or match.
var ErrNotFound = errors.New("faceit: not found")

// StatusError is an error status returned by FACEIT. It matches ErrNotFound
// for 404.
type StatusError struct {
	Op     string // what was asked, like "get match"; never holds IDs or nicknames
	Status int
	Code   string // FACEIT error code if the body has one, like err_nf0 or invalid_token
}

func (e *StatusError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("faceit: %s: status %d (%s)", e.Op, e.Status, e.Code)
	}
	return fmt.Sprintf("faceit: %s: status %d", e.Op, e.Status)
}

// Is makes errors.Is(err, ErrNotFound) true for a 404.
func (e *StatusError) Is(target error) bool {
	return target == ErrNotFound && e.Status == http.StatusNotFound
}

// retryable reports whether the request may succeed if tried again.
func (e *StatusError) retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// Client calls the FACEIT Data API. It is safe for concurrent use; all calls
// share one rate limit.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	limiter *limiter
	log     *slog.Logger
	// sleep waits for d or until ctx is done; tests replace it.
	sleep func(ctx context.Context, d time.Duration) error
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another server, such as a test one.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimSuffix(u, "/") }
}

// WithHTTPClient replaces the HTTP client, which by default has a 10 s
// timeout and honours HTTPS_PROXY.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.http = hc }
}

// WithLogger sets the logger for retries and request timings.
func WithLogger(log *slog.Logger) Option {
	return func(c *Client) { c.log = log }
}

// New returns a client with the server-side API key.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: requestTimeout},
		limiter: newLimiter(rateInterval, rateBurst, time.Now),
		log:     slog.New(slog.DiscardHandler),
		sleep:   sleepCtx,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// get fetches path with query and decodes the JSON response into out. op
// names the call in errors and logs, which carry neither the URL nor its
// parameters: they hold nicknames and IDs.
func (c *Client) get(ctx context.Context, op, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	for attempt := 1; ; attempt++ {
		r := c.try(ctx, op, u, out)
		if r.err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return fmt.Errorf("faceit: %s: %w", op, ctx.Err())
		}
		if !r.retry || attempt == maxAttempts {
			return r.err
		}
		wait := max(r.wait, retryWait<<(attempt-1))
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
			return r.err
		}
		c.log.Warn("faceit request failed, retrying", "op", op, "attempt", attempt, "wait", wait, "err", r.err)
		if err := c.sleep(ctx, wait); err != nil {
			return fmt.Errorf("faceit: %s: %w", op, err)
		}
	}
}

// outcome is the result of one try of a request.
type outcome struct {
	err   error
	retry bool          // a new try may succeed
	wait  time.Duration // the pause FACEIT asked for, if any
}

// try makes one attempt of a request.
func (c *Client) try(ctx context.Context, op, u string, out any) outcome {
	if err := c.limiter.wait(ctx, c.sleep); err != nil {
		return outcome{err: fmt.Errorf("faceit: %s: %w", op, err)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return outcome{err: fmt.Errorf("faceit: %s: build request: %w", op, err)}
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		// *url.Error repeats the whole URL with the nickname or IDs in it.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return outcome{err: fmt.Errorf("faceit: %s: %w", op, err), retry: true}
	}
	defer resp.Body.Close()
	c.log.Debug("faceit request", "op", op, "status", resp.StatusCode, "duration", time.Since(start))

	if resp.StatusCode != http.StatusOK {
		se := &StatusError{Op: op, Status: resp.StatusCode, Code: errorCode(resp.Body)}
		return outcome{err: se, retry: se.retryable(), wait: retryAfter(resp.Header)}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize+1))
	if err != nil {
		return outcome{err: fmt.Errorf("faceit: %s: read response: %w", op, err), retry: true}
	}
	if len(body) > maxBodySize {
		return outcome{err: fmt.Errorf("faceit: %s: response is larger than %d bytes", op, maxBodySize)}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return outcome{err: fmt.Errorf("faceit: %s: decode response: %w", op, err)}
	}
	return outcome{}
}

// errorCode reads the FACEIT error code from an error body. Data API errors
// look like {"errors":[{"code":"err_nf0",...}]}, a rejected key like
// {"error":"invalid_token",...}.
func errorCode(body io.Reader) string {
	data, err := io.ReadAll(io.LimitReader(body, 64<<10))
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return ""
	}
	var e struct {
		Error  string `json:"error"`
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if json.Unmarshal(data, &e) != nil {
		return ""
	}
	if len(e.Errors) > 0 {
		return e.Errors[0].Code
	}
	return e.Error
}

// retryAfter returns the wait FACEIT asks for, from Retry-After or
// RateLimit-Reset in seconds, capped at maxRetryWait.
func retryAfter(h http.Header) time.Duration {
	for _, name := range []string{"Retry-After", "RateLimit-Reset"} {
		if s, err := strconv.Atoi(strings.TrimSpace(h.Get(name))); err == nil && s > 0 {
			return min(time.Duration(s)*time.Second, maxRetryWait)
		}
	}
	return 0
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
