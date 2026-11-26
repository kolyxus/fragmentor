// Package llm calls Polza's non-streaming text endpoint. Its inputs are text
// messages; the caller owns their contents. See docs/notes/polza.md.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Client struct {
	endpoint         string
	apiKey           string
	model            string
	timeout          time.Duration
	maxTokens        int
	maxRequestBytes  int
	maxResponseBytes int
	maxRetries       int
	http             *http.Client
	log              *slog.Logger
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Usage holds provider-reported amounts. Nil means the amount is unknown.
type Usage struct {
	PromptTokens     *int64   `json:"prompt_tokens"`
	CompletionTokens *int64   `json:"completion_tokens"`
	CostRUB          *float64 `json:"cost_rub"`
}

type Result struct {
	Text  string
	Usage Usage
	// HTTPAttempts counts POST attempts submitted to the HTTP client, even
	// when the outcome is unknown. Local validation failures cost no attempt.
	HTTPAttempts int
}

// Timeout is the configured deadline budget for completion and correction.
func (c *Client) Timeout() time.Duration { return c.timeout }

// HTTPError reports the status without disclosing the provider's error body.
type HTTPError struct{ StatusCode int }

func (e *HTTPError) Error() string { return fmt.Sprintf("llm: HTTP %d", e.StatusCode) }

// Complete returns a single finished text. Usage can also be available when
// the provider billed a completion that yielded no usable text.
func (c *Client) Complete(ctx context.Context, messages []Message) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(messages) == 0 {
		return Result{}, errors.New("llm: messages are required")
	}
	// Reject oversized input before allocating its JSON representation. JSON
	// escaping and field names are included in the exact check after Marshal.
	remaining := c.maxRequestBytes
	for _, m := range messages {
		if len(m.Content) > remaining {
			return Result{}, errors.New("llm: request exceeds byte limit")
		}
		remaining -= len(m.Content)
		switch m.Role {
		case "system", "developer", "user", "assistant":
		default:
			return Result{}, errors.New("llm: invalid message role")
		}
		if strings.TrimSpace(m.Content) == "" || !utf8.ValidString(m.Content) {
			return Result{}, errors.New("llm: invalid message content")
		}
	}
	body, err := json.Marshal(struct {
		Model     string    `json:"model"`
		Messages  []Message `json:"messages"`
		MaxTokens int       `json:"max_tokens"`
		Stream    bool      `json:"stream"`
	}{c.model, messages, c.maxTokens, false})
	if err != nil {
		return Result{}, fmt.Errorf("llm: encode request: %w", err)
	}
	if len(body) > c.maxRequestBytes {
		return Result{}, errors.New("llm: request exceeds byte limit")
	}
	var result Result
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
		if err != nil {
			return result, errors.New("llm: invalid request")
		}
		// Only this loop may replay a body. In particular, do not give the
		// HTTP/2 transport a way to rewind a paid POST.
		req.GetBody = nil
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		result.HTTPAttempts++
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return result, ctx.Err()
			}
			return result, errors.New("llm: HTTP request failed")
		}
		if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			reply, err := c.readResult(ctx, resp.Body)
			reply.HTTPAttempts = result.HTTPAttempts
			return reply, err
		}
		resp.Body.Close()
		if err := ctx.Err(); err != nil {
			return result, err
		}
		status := &HTTPError{StatusCode: resp.StatusCode}
		if resp.StatusCode != http.StatusTooManyRequests || result.HTTPAttempts > c.maxRetries {
			return result, status
		}
		delay := retryDelay(resp.Header.Get("Retry-After"))
		deadline, _ := ctx.Deadline()
		if delay >= time.Until(deadline) {
			return result, status
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// Retry-After uses whole seconds or an HTTP date. Saturate large valid
// values so an overflow cannot turn a long wait into an immediate retry.
func retryDelay(value string) time.Duration {
	if value != "" && strings.Trim(value, "0123456789") == "" {
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil || n > uint64(math.MaxInt64/int64(time.Second)) {
			return time.Duration(math.MaxInt64)
		}
		return time.Duration(n) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(time.Until(date), 0)
	}
	return time.Second
}

func (c *Client) readResult(ctx context.Context, body io.Reader) (Result, error) {
	data, err := io.ReadAll(io.LimitReader(body, int64(c.maxResponseBytes)+1))
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, errors.New("llm: response body read failed")
	}
	if len(data) > c.maxResponseBytes {
		return Result{}, errors.New("llm: response exceeds byte limit")
	}
	var wire struct {
		Choices json.RawMessage `json:"choices"`
		Error   json.RawMessage `json:"error"`
		Usage   struct {
			Usage
			Cost *float64 `json:"cost"`
		} `json:"usage"`
	}
	if !utf8.Valid(data) || json.Unmarshal(data, &wire) != nil {
		return Result{}, errors.New("llm: invalid response JSON or usage")
	}
	u := wire.Usage.Usage
	if u.CostRUB == nil {
		u.CostRUB = wire.Usage.Cost
	}
	if (u.PromptTokens != nil && *u.PromptTokens < 0) ||
		(u.CompletionTokens != nil && *u.CompletionTokens < 0) || (u.CostRUB != nil && *u.CostRUB < 0) {
		return Result{}, errors.New("llm: invalid usage")
	}
	// Limit log fields to numeric usage, even for a rejected completion.
	attrs := []slog.Attr{}
	if u.PromptTokens != nil {
		attrs = append(attrs, slog.Int64("prompt_tokens", *u.PromptTokens))
	}
	if u.CompletionTokens != nil {
		attrs = append(attrs, slog.Int64("completion_tokens", *u.CompletionTokens))
	}
	if u.CostRUB != nil {
		attrs = append(attrs, slog.Float64("cost_rub", *u.CostRUB))
	}
	c.log.LogAttrs(ctx, slog.LevelInfo, "llm usage", attrs...)
	var choices []struct {
		FinishReason string  `json:"finish_reason"`
		Message      Message `json:"message"`
	}
	result := Result{Usage: u}
	if len(wire.Error) != 0 && string(wire.Error) != "null" {
		return result, errors.New("llm: provider returned an error")
	}
	if json.Unmarshal(wire.Choices, &choices) != nil || len(choices) != 1 ||
		choices[0].FinishReason != "stop" || choices[0].Message.Role != "assistant" ||
		strings.TrimSpace(choices[0].Message.Content) == "" {
		return result, errors.New("llm: expected one finished text response")
	}
	result.Text = choices[0].Message.Content
	return result, nil
}
