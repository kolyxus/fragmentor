package review

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
)

// modelInput contains only the picks the engine selected. IDs are local to
// this review. Display strings use the same units and precision as Render.
type modelInput struct {
	Language i18n.Lang   `json:"language"`
	Items    []modelItem `json:"items"`
	Task     *reviewTask `json:"task,omitempty"`
}

type reviewTask struct {
	FindingID string `json:"finding_id"`
	DrillID   string `json:"drill_id"`
}

type modelItem struct {
	FindingID string          `json:"finding_id"`
	Kind      findings.Kind   `json:"kind"`
	Rule      findings.RuleID `json:"rule"`
	Variant   string          `json:"variant,omitempty"`
	Summary   modelSummary    `json:"summary"`
	Moments   []modelMoment   `json:"moments"`
}

type modelSummary struct {
	ID string `json:"id"`
	comparisonValues
	Habit   bool           `json:"habit,omitempty"`
	Related []relatedCount `json:"related,omitempty"`
	line    string
}

type relatedCount struct {
	FindingID string          `json:"finding_id"`
	Rule      findings.RuleID `json:"rule"`
	Count     string          `json:"count"`
}

type modelMoment struct {
	ID     string `json:"id"`
	Round  int    `json:"round"`
	Clock  string `json:"clock"`
	Detail string `json:"detail"`
	line   string // includes the local demo command, omitted from the request
}

func (r renderer) modelInput() modelInput {
	in := modelInput{Language: r.lang, Items: []modelItem{}}
	for _, pk := range r.f.Review.Mistakes {
		in.Items = append(in.Items, r.modelItem(pk))
	}
	if s := r.f.Review.Strength; s != nil {
		in.Items = append(in.Items, r.modelItem(*s))
	}
	if task := r.f.Review.Task; task != nil {
		in.Task = &reviewTask{FindingID: task.Finding, DrillID: task.Drill}
	}
	return in
}

func (r renderer) modelItem(pk findings.PickFacts) modelItem {
	f := r.byID[pk.Finding]
	summary := modelSummary{
		ID: f.ID + ".summary", comparisonValues: r.comparison(f),
		Habit: f.Habit, line: r.pick(pk),
	}
	for _, id := range pk.With {
		w := r.byID[id]
		summary.Related = append(summary.Related, relatedCount{FindingID: id, Rule: w.Rule, Count: r.count(w)})
	}
	item := modelItem{FindingID: f.ID, Kind: f.Kind, Rule: f.Rule, Variant: f.Variant, Summary: summary, Moments: []modelMoment{}}
	for i, e := range f.BestMoments(MaxMoments) {
		detail := r.momentDetail(f, e)
		item.Moments = append(item.Moments, modelMoment{
			ID: fmt.Sprintf("%s.m%d", f.ID, i+1), Round: e.Round, Clock: r.clock(e.Clock, e.Planted),
			Detail: detail, line: r.momentLine(e, detail),
		})
	}
	return item
}

// validate enforces the temporary fragment policy, independently of JSON
// decoding. Only Go-sourced lines reach the renderer.
func (in modelInput) validate(reply modelResponse) (map[string]string, error) {
	if len(reply.Items) != len(in.Items) {
		return nil, errors.New("include exactly one item for each input finding")
	}
	if (reply.Task == nil) != (in.Task == nil) || in.Task != nil && *reply.Task != *in.Task {
		return nil, errors.New("copy the input task exactly, or omit it when absent")
	}
	sections := make(map[string]string, len(in.Items))
	for i, src := range in.Items {
		item := reply.Items[i]
		if item.FindingID != src.FindingID {
			return nil, errors.New("use the input finding IDs in their original order")
		}
		ids := item.FragmentIDs
		if len(ids) == 0 || ids[0] != src.Summary.ID {
			return nil, errors.New("start each item with its own summary ID")
		}
		if len(src.Moments) > 0 && len(ids) == 1 {
			return nil, errors.New("include a moment ID when evidence is available")
		}
		var section strings.Builder
		section.WriteString(src.Summary.line)
		remaining := src.Moments
		for _, id := range ids[1:] {
			j := slices.IndexFunc(remaining, func(m modelMoment) bool { return m.ID == id })
			if j < 0 {
				return nil, errors.New("use only this finding's distinct moment IDs in chronological order")
			}
			section.WriteString("\n   · " + remaining[j].line)
			remaining = remaining[j+1:]
		}
		sections[src.FindingID] = section.String()
	}
	return sections, nil
}
