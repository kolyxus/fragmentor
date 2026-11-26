package review

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kolyxus/fragmentor/bot/content"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/llm"
)

type fakeCompletion func(context.Context, []llm.Message) (llm.Result, error)

func (f fakeCompletion) Complete(ctx context.Context, msgs []llm.Message) (llm.Result, error) {
	return f(ctx, msgs)
}

func (fakeCompletion) Timeout() time.Duration { return time.Minute }

// Simulate a model that copies the required references and chooses the first
// episode. Read only the JSON sent to Complete, as the provider would.
func selectFirstMoment(t *testing.T, msgs []llm.Message) string {
	t.Helper()
	var input struct {
		Items []struct {
			FindingID string `json:"finding_id"`
			Summary   struct{ ID string }
			Moments   []struct{ ID string }
		}
		Task *reviewTask
	}
	if err := json.Unmarshal([]byte(msgs[1].Content), &input); err != nil {
		t.Fatal(err)
	}
	response := modelResponse{Task: input.Task}
	for _, item := range input.Items {
		ids := []string{item.Summary.ID}
		if len(item.Moments) > 0 {
			ids = append(ids, item.Moments[0].ID)
		}
		response.Items = append(response.Items, responseItem{FindingID: item.FindingID, FragmentIDs: ids})
	}
	wire, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return string(wire)
}

func TestGenerate(t *testing.T) {
	for _, lang := range i18n.Langs {
		for _, tc := range []struct {
			name     string
			outcomes []string
			fallback bool
		}{
			{"first success", []string{"valid"}, false},
			{"correct structure", []string{"syntax", "valid"}, false},
			{"correct content", []string{"content", "valid"}, false},
			{"both invalid", []string{"syntax", "content"}, true},
			{"provider error", []string{"error"}, true},
			{"failed correction", []string{"content", "error"}, true},
		} {
			t.Run(string(lang)+"/"+tc.name, func(t *testing.T) {
				f := facts()
				c := content.Default()
				template := Render(f, lang, c)
				start := time.Now()
				var firstDeadline time.Time
				var firstPayload string
				var usage []llm.Usage
				var posts int
				const privateMarker = "invalid-private-model-output"
				client := fakeCompletion(func(ctx context.Context, msgs []llm.Message) (llm.Result, error) {
					idx := len(usage)
					if idx >= len(tc.outcomes) {
						t.Fatal("unexpected extra completion")
					}
					deadline, ok := ctx.Deadline()
					if !ok || deadline.Before(start) || deadline.After(time.Now().Add(time.Minute)) {
						t.Fatal("missing configured deadline")
					}
					if idx == 0 {
						if len(msgs) != 2 || msgs[0].Role != "system" || msgs[1].Role != "user" {
							t.Fatal("missing instruction or compact input")
						}
						firstDeadline, firstPayload = deadline, msgs[1].Content
					} else {
						if deadline != firstDeadline || len(msgs) != 3 || msgs[1].Content != firstPayload || msgs[2].Content == "" {
							t.Fatal("correction exceeded original deadline or lost input/instruction")
						}
						for _, msg := range msgs {
							if strings.Contains(msg.Content, privateMarker) {
								t.Fatal("replayed rejected provider output")
							}
						}
					}
					reply := llm.Result{HTTPAttempts: 2}
					tokens, cost := int64(idx+1), float64(idx)/10
					reply.Usage = llm.Usage{PromptTokens: &tokens, CostRUB: &cost}
					var err error
					switch tc.outcomes[idx] {
					case "valid":
						reply.Text = selectFirstMoment(t, msgs)
					case "syntax":
						reply.Text = privateMarker
					case "content":
						reply.Text = `{"items":[{"finding_id":"` + privateMarker + `","fragment_ids":["unknown"]}]}`
					case "error":
						reply.Usage = llm.Usage{CompletionTokens: &tokens}
						reply.HTTPAttempts = 1
						err = errors.New("failed completion")
					}
					usage = append(usage, reply.Usage)
					posts += reply.HTTPAttempts
					return reply, err
				})
				got := Generate(t.Context(), client, f, lang, c)
				if got.Fallback != tc.fallback || len(usage) != len(tc.outcomes) || !reflect.DeepEqual(got.Usage, usage) || got.HTTPAttempts != posts {
					t.Fatalf("fallback=%v, completions=%d, attempts=%d, incorrect outcome or accounting", got.Fallback, len(got.Usage), got.HTTPAttempts)
				}
				if tc.fallback {
					if got.Text != template {
						t.Fatal("fallback differs from complete template")
					}
				} else if !strings.Contains(got.Text, "demo_gototick 15962") || strings.Contains(got.Text, "demo_gototick 50994") ||
					!strings.Contains(got.Text, c.Drills()[0].Task.In(lang)) {
					t.Fatal("accepted review did not use the selected evidence and original task")
				}
			})
		}
	}
}

func TestGenerateSkipsUnavailableWork(t *testing.T) {
	for _, mode := range []string{"empty selection", "expired", "cancel before correction"} {
		t.Run(mode, func(t *testing.T) {
			// A caller's shorter budget covers both generations.
			deadline := time.Now().Add(20 * time.Second)
			ctx, cancel := context.WithDeadline(t.Context(), deadline)
			defer cancel()
			f := facts()
			if mode == "empty selection" {
				f.Review = findings.ReviewFacts{}
			}
			if mode == "expired" {
				cancel()
			}
			var calls int
			client := fakeCompletion(func(ctx context.Context, _ []llm.Message) (llm.Result, error) {
				calls++
				if d, ok := ctx.Deadline(); !ok || !d.Equal(deadline) {
					t.Fatal("caller deadline was replaced")
				}
				cancel()
				return llm.Result{Text: "incomplete", HTTPAttempts: 1}, nil
			})
			got := Generate(ctx, client, f, i18n.EN, content.Default())
			want := 0
			if mode == "cancel before correction" {
				want = 1
			}
			if !got.Fallback || got.Text != Render(f, i18n.EN, content.Default()) || calls != want || len(got.Usage) != want || got.HTTPAttempts != want {
				t.Fatalf("fallback=%v, calls=%d, attempts=%d", got.Fallback, calls, got.HTTPAttempts)
			}
		})
	}
}
