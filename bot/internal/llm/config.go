package llm

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// NewClient reads the LLM settings on demand. Pass os.Getenv for the process
// environment. Creating a client does not contact the provider.
func NewClient(getenv func(string) string, logger *slog.Logger) (*Client, error) {
	base, err := url.Parse(strings.TrimSpace(getenv("LLM_BASE_URL")))
	if err != nil || base.Scheme != "https" || base.Hostname() == "" || base.User != nil ||
		base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
		return nil, errors.New("llm: LLM_BASE_URL must be an HTTPS base URL without credentials, query or fragment")
	}
	key := strings.TrimSpace(getenv("LLM_API_KEY"))
	if key == "" || strings.ContainsAny(key, "\r\n") {
		return nil, errors.New("llm: invalid LLM_API_KEY")
	}
	model := strings.TrimSpace(getenv("LLM_MODEL"))
	if model == "" {
		return nil, errors.New("llm: LLM_MODEL is required")
	}
	timeout := 30 * time.Second
	if value := strings.TrimSpace(getenv("LLM_TIMEOUT")); value != "" {
		timeout, err = time.ParseDuration(value)
		if err != nil || timeout <= 0 {
			return nil, errors.New("llm: LLM_TIMEOUT must be a positive duration")
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	c := &Client{
		endpoint: base.JoinPath("chat", "completions").String(),
		apiKey:   key,
		model:    model,
		timeout:  timeout,
		log:      logger,
		http: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
	for _, setting := range []struct {
		name               string
		target             *int
		fallback, min, max int
	}{
		{"LLM_MAX_TOKENS", &c.maxTokens, 1024, 1, 1<<31 - 1},
		{"LLM_MAX_REQUEST_BYTES", &c.maxRequestBytes, 64 << 10, 1, 1<<31 - 1},
		{"LLM_MAX_RESPONSE_BYTES", &c.maxResponseBytes, 1 << 20, 1, 1<<31 - 1},
		{"LLM_MAX_RETRIES", &c.maxRetries, 1, 0, 1},
	} {
		value := setting.fallback
		if raw := strings.TrimSpace(getenv(setting.name)); raw != "" {
			value, err = strconv.Atoi(raw)
			if err != nil || value < setting.min || value > setting.max {
				return nil, fmt.Errorf("llm: %s must be between %d and %d", setting.name, setting.min, setting.max)
			}
		}
		*setting.target = value
	}
	return c, nil
}
