package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testKey   = "synthetic-auth-marker"
	testInput = "synthetic-private-input"
	testText  = "synthetic-private-output"
	choice    = `"choices":[{"message":{"role":"assistant","content":"` + testText + `"},"finish_reason":"stop"}]`
	success   = "{" + choice + "}"
)

var messages = []Message{{Role: "user", Content: testInput}}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func settings(base string) map[string]string {
	return map[string]string{"LLM_BASE_URL": base, "LLM_MODEL": "test/model", "LLM_API_KEY": testKey}
}

func testClient(t *testing.T, overrides map[string]string, handler http.HandlerFunc) (*Client, *bytes.Buffer) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	env := settings(server.URL + "/api/v1/")
	maps.Copy(env, overrides)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	client, err := NewClient(func(k string) string { return env[k] }, logger)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = server.Client().Transport
	return client, &logs
}

func assertPrivate(t *testing.T, diagnostic string) {
	t.Helper()
	for _, s := range []string{testKey, "authorization", "bearer ", testInput, testText} {
		if strings.Contains(strings.ToLower(diagnostic), s) {
			t.Errorf("diagnostic contains private marker %q", s)
		}
	}
}

func TestComplete(t *testing.T) {
	client, logs := testClient(t, map[string]string{"LLM_MAX_TOKENS": "27"}, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.RequestURI() != "/api/v1/chat/completions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey || r.Header.Get("Content-Type") != "application/json" {
			t.Error("unexpected request headers")
		}
		var req struct {
			Model     string
			MaxTokens int `json:"max_tokens"`
			Messages  []struct{ Role, Content string }
			Stream    bool
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if req.Model != "test/model" || req.MaxTokens != 27 || req.Stream || len(req.Messages) != 1 ||
			req.Messages[0].Role != "user" || req.Messages[0].Content != testInput {
			t.Error("unexpected completion payload")
		}
		fmt.Fprint(w, "{"+choice+`,"usage":{"prompt_tokens":8,"completion_tokens":3,"cost_rub":0.007}}`)
	})
	got, err := client.Complete(t.Context(), messages)
	if err != nil || got.Text != testText {
		t.Fatalf("completion failed or returned the wrong text: %v", err)
	}
	if u := got.Usage; u.PromptTokens == nil || *u.PromptTokens != 8 || u.CompletionTokens == nil ||
		*u.CompletionTokens != 3 || u.CostRUB == nil || *u.CostRUB != 0.007 {
		t.Error("unexpected returned usage")
	}
	assertPrivate(t, logs.String())
	var usageLogged bool
	for line := range strings.Lines(logs.String()) {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatal(err)
		}
		if record["prompt_tokens"] == 8.0 && record["completion_tokens"] == 3.0 && record["cost_rub"] == 0.007 {
			usageLogged = true
		}
	}
	if !usageLogged {
		t.Error("missing usage fields in structured logs")
	}
}

func TestUsagePresence(t *testing.T) {
	zero := int64(0)
	free, paid := 0.0, 0.125
	for _, tc := range []struct {
		name, usage string
		want        Usage
	}{
		{"absent", "", Usage{}},
		{"partial zero", `,"usage":{"prompt_tokens":null,"completion_tokens":0,"cost_rub":0}`, Usage{CompletionTokens: &zero, CostRUB: &free}},
		{"cost alias", `,"usage":{"cost":0.125}`, Usage{CostRUB: &paid}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, logs := testClient(t, nil, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, "{"+choice+tc.usage+"}")
			})
			got, err := client.Complete(t.Context(), messages)
			if err != nil || !reflect.DeepEqual(got.Usage, tc.want) {
				t.Fatalf("incorrect usage or error: %v", err)
			}
			if tc.want.CostRUB == nil && strings.Contains(logs.String(), `"cost_rub":0`) {
				t.Error("absent cost logged as zero")
			}
		})
	}
}

func TestHTTPFailures(t *testing.T) {
	for _, code := range []int{400, 401, 402, 403, 408, 429, 500, 502, 503, 504} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			var calls atomic.Int32
			client, logs := testClient(t, nil, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "0")
				http.Error(w, testInput+" "+testKey, code)
			})
			result, err := client.Complete(t.Context(), messages)
			var status *HTTPError
			if !errors.As(err, &status) || status.StatusCode != code {
				t.Fatalf("expected typed HTTP %d error, got %v", code, err)
			}
			want := int32(1)
			if code == 429 {
				want = 2
			}
			if got := calls.Load(); got != want {
				t.Errorf("calls = %d, want %d", got, want)
			}
			if result.HTTPAttempts != int(want) {
				t.Errorf("reported %d HTTP attempts, want %d", result.HTTPAttempts, want)
			}
			assertPrivate(t, err.Error()+logs.String())
		})
	}
}

func TestRateLimitRetry(t *testing.T) {
	var calls atomic.Int32
	client, _ := testClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []Message }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !reflect.DeepEqual(req.Messages, messages) {
			t.Error("unexpected request body across retries")
		}
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, success)
	})
	started := time.Now()
	got, err := client.Complete(t.Context(), messages)
	if err != nil || got.Text != testText || calls.Load() != 2 || got.HTTPAttempts != 2 {
		t.Fatalf("retry failed: calls=%d, err=%v", calls.Load(), err)
	}
	if time.Since(started) < time.Second {
		t.Error("retried before Retry-After delay elapsed")
	}
}

func TestRateLimitRetryRestrictions(t *testing.T) {
	for _, tc := range []struct{ name, retries, delay string }{
		{"disabled", "0", "0"},
		{"date beyond deadline", "1", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)},
		{"seconds beyond deadline", "1", "86400"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			client, _ := testClient(t, map[string]string{"LLM_MAX_RETRIES": tc.retries, "LLM_TIMEOUT": "5s"}, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", tc.delay)
				w.WriteHeader(429)
			})
			_, err := client.Complete(t.Context(), messages)
			var status *HTTPError
			if !errors.As(err, &status) || status.StatusCode != 429 || calls.Load() != 1 {
				t.Errorf("calls=%d, err=%v", calls.Load(), err)
			}
		})
	}
}

func TestInvalidResponses(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"broken JSON", "{" + testInput},
		{"extra JSON", success + " {}"},
		{"empty choices", `{"choices":[]}`},
		{"empty text", strings.Replace(success, testText, " ", 1)},
		{"wrong role", strings.Replace(success, "assistant", "user", 1)},
		{"provider error", "{" + choice + `,"error":{"message":"` + testInput + `"}}`},
		{"negative usage", "{" + choice + `,"usage":{"prompt_tokens":-1}}`},
		{"invalid money", "{" + choice + `,"usage":{"cost_rub":"private"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			client, logs := testClient(t, nil, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				fmt.Fprint(w, tc.body)
			})
			got, err := client.Complete(t.Context(), messages)
			if err == nil || got.Text != "" || calls.Load() != 1 {
				t.Fatalf("expected one failed attempt, calls=%d err=%v", calls.Load(), err)
			}
			assertPrivate(t, err.Error()+logs.String())
		})
	}
}

func TestCancelWhileWaitingForRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var calls atomic.Int32
	client, _ := testClient(t, map[string]string{"LLM_TIMEOUT": "10s"}, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 1 {
			t.Error("rate-limited request was retried after cancellation")
			return
		}
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(429)
		w.(http.Flusher).Flush()
		// Closing the unread response body closes this HTTP/1 connection.
		// The client has seen the rate limit and can now abandon its wait.
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
			t.Error("client did not release the rate-limit response")
		}
		cancel()
	})
	_, err := client.Complete(ctx, messages)
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Errorf("calls=%d err=%v", calls.Load(), err)
	}
}

func TestTruncatedCompletionStillAccountsForUsage(t *testing.T) {
	client, logs := testClient(t, nil, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "{"+strings.Replace(choice, `"stop"`, `"length"`, 1)+`,"usage":{"completion_tokens":9,"cost_rub":0.14}}`)
	})
	got, err := client.Complete(t.Context(), messages)
	if err == nil || got.Text != "" {
		t.Fatal("truncated completion accepted")
	}
	if got.Usage.CostRUB == nil || *got.Usage.CostRUB != 0.14 {
		t.Error("reported charge absent from result")
	}
	if !strings.Contains(logs.String(), `"cost_rub":0.14`) || !strings.Contains(logs.String(), `"completion_tokens":9`) {
		t.Error("reported charges lost on incomplete generation")
	}
	assertPrivate(t, err.Error()+logs.String())
}

func TestByteLimits(t *testing.T) {
	for _, tc := range []struct {
		key   string
		calls int32
	}{
		{"LLM_MAX_REQUEST_BYTES", 0}, {"LLM_MAX_RESPONSE_BYTES", 1},
	} {
		t.Run(tc.key, func(t *testing.T) {
			var calls atomic.Int32
			client, _ := testClient(t, map[string]string{tc.key: "16"}, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				fmt.Fprint(w, success)
			})
			if got, err := client.Complete(t.Context(), messages); err == nil || calls.Load() != tc.calls || got.HTTPAttempts != int(tc.calls) {
				t.Errorf("calls=%d, err=%v", calls.Load(), err)
			}
		})
	}
}

func TestNoReplayOnLostResponseOrRedirect(t *testing.T) {
	for _, mode := range []string{"lost response", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			client, logs := testClient(t, nil, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) > 1 {
					t.Error("unexpected second request")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_, _ = io.Copy(io.Discard, r.Body)
				if mode == "redirect" {
					http.Redirect(w, r, "/"+testInput, http.StatusTemporaryRedirect)
					return
				}
				conn, _, err := http.NewResponseController(w).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				conn.Close()
			})
			transport := client.http.Transport
			client.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.GetBody != nil {
					t.Error("HTTP transport could replay a paid POST body")
				}
				return transport.RoundTrip(r)
			})
			_, err := client.Complete(t.Context(), messages)
			if err == nil || calls.Load() != 1 {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
			assertPrivate(t, err.Error()+logs.String())
		})
	}
}

func TestCancellation(t *testing.T) {
	for _, mode := range []string{"before send", "during request", "client timeout"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var calls atomic.Int32
			client, _ := testClient(t, map[string]string{"LLM_TIMEOUT": "500ms"}, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				if mode == "during request" {
					cancel()
				}
				select {
				case <-r.Context().Done():
				case <-time.After(4 * time.Second):
				}
			})
			wantErr := context.Canceled
			wantCalls := int32(1)
			switch mode {
			case "before send":
				cancel()
				wantCalls = 0
			case "client timeout":
				wantErr = context.DeadlineExceeded
			}
			_, err := client.Complete(ctx, messages)
			if !errors.Is(err, wantErr) || calls.Load() != wantCalls {
				t.Errorf("calls=%d, err=%v, want %d, %v", calls.Load(), err, wantCalls, wantErr)
			}
		})
	}
}

func TestClientSettings(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"LLM_BASE_URL", "http://example.invalid"}, {"LLM_BASE_URL", ""}, {"LLM_MODEL", ""}, {"LLM_API_KEY", ""},
		{"LLM_TIMEOUT", "bad"}, {"LLM_TIMEOUT", "0s"}, {"LLM_MAX_TOKENS", "0"}, {"LLM_MAX_REQUEST_BYTES", "-1"},
		{"LLM_MAX_RESPONSE_BYTES", "0"}, {"LLM_MAX_RETRIES", "2"},
	} {
		t.Run(tc.key+"/"+tc.value, func(t *testing.T) {
			env := settings("https://example.invalid")
			env[tc.key] = tc.value
			_, err := NewClient(func(key string) string { return env[key] }, slog.New(slog.DiscardHandler))
			if err == nil {
				t.Fatal("invalid setting accepted")
			}
			assertPrivate(t, err.Error())
		})
	}
}
