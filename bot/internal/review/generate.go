package review

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kolyxus/fragmentor/bot/content"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
	"github.com/kolyxus/fragmentor/bot/internal/llm"
)

type completionClient interface {
	Complete(context.Context, []llm.Message) (llm.Result, error)
	Timeout() time.Duration
}

// GenerationResult includes every call to Complete, even when the response
// is discarded. Missing usage fields mean that the provider cost or token
// count is unknown. HTTPAttempts includes the client's rate-limit retries.
type GenerationResult struct {
	Text         string
	Fallback     bool
	Usage        []llm.Usage
	HTTPAttempts int
}

// This instruction exercises the fragment contract locally until the coaching
// prompt and wording can be evaluated after checkpoint 2.
const fragmentInstruction = `Return exactly one JSON object with an "items" array.
Copy every finding_id once in input order. Each item has only "finding_id" and "fragment_ids".
Start fragment_ids with that item's summary.id. If it has moments, choose one or more of their IDs in input order, without repeats.
If the input includes "task", copy that object exactly, with only "finding_id" and "drill_id". Otherwise omit "task".
Output only these references. Do not include prose, numbers, new findings or Markdown.`

// Generate uses one shared deadline for both completions. On a client error
// or a final validation failure it returns the complete template review.
func Generate(ctx context.Context, client completionClient, f findings.Facts, lang i18n.Lang, c *content.Catalog) GenerationResult {
	ctx, cancel := context.WithTimeout(ctx, client.Timeout())
	defer cancel()
	r := newRenderer(f, lang, c)
	input := r.modelInput()
	result := GenerationResult{Text: r.render(nil), Fallback: true}
	if len(input.Items) == 0 || ctx.Err() != nil {
		return result
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return result
	}
	messages := []llm.Message{
		{Role: "system", Content: fragmentInstruction},
		{Role: "user", Content: string(payload)},
	}
	for attempt := 0; attempt < 2 && ctx.Err() == nil; attempt++ {
		reply, err := client.Complete(ctx, messages)
		result.Usage = append(result.Usage, reply.Usage)
		result.HTTPAttempts += reply.HTTPAttempts
		if err != nil || ctx.Err() != nil {
			return result
		}
		response, err := decodeResponse(reply.Text)
		correction := "Return the exact JSON schema in the system instruction. Check the required fields and types."
		if err == nil {
			sections, err := input.validate(response)
			if err == nil && ctx.Err() == nil {
				result.Text = r.render(sections)
				result.Fallback = false
				return result
			}
			correction = "Check each finding and its fragment IDs against the same input item, preserve their order, and copy the task exactly."
		}
		if attempt == 0 {
			messages = append(messages, llm.Message{Role: "user", Content: correction})
		}
	}
	return result
}
