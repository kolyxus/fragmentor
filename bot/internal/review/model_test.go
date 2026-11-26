package review

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/kolyxus/fragmentor/bot/content"
	"github.com/kolyxus/fragmentor/bot/internal/findings"
	"github.com/kolyxus/fragmentor/bot/internal/i18n"
)

func TestModelInput(t *testing.T) {
	f := facts()
	// Only the related finding supports the engine's selected task.
	f.Findings[0].Drills = nil
	f.Findings[2].Drills = []string{"demo_review"}
	f.Events.Kills = []findings.KillFact{{Weapon: "unselected-event"}}
	f.Comparisons = append(f.Comparisons, findings.Comparison{Measure: "unselected-comparison"})
	f.Findings = append(f.Findings, findings.FactFinding{ID: "unselected-finding"})
	for _, lang := range i18n.Langs {
		t.Run(string(lang), func(t *testing.T) {
			in := newRenderer(f, lang, content.Default()).modelInput()
			if in.Language != lang || len(in.Items) != 3 || in.Items[0].FindingID != "f1" ||
				in.Items[1].FindingID != "f2" || in.Items[2].FindingID != "f4" {
				t.Fatalf("incorrect selection: %+v", in.Items)
			}
			s := in.Items[0].Summary
			if s.Model != findings.ModelShare || s.Observed != "9" || s.Lobby != "12%" ||
				s.ID != "f1.summary" || len(s.Related) != 1 || s.Related[0].FindingID != "f3" ||
				s.Related[0].Rule != findings.UntradedDeath || !in.Items[1].Summary.Habit {
				t.Fatalf("wrong finding summary: %+v", s)
			}
			if in.Task == nil || *in.Task != (reviewTask{FindingID: "f1", DrillID: "demo_review"}) {
				t.Fatalf("task from related finding: %+v", in.Task)
			}
			m := in.Items[0].Moments
			if len(m) != MaxMoments || m[0].Round != 3 || m[0].Clock != "1:12" ||
				m[1].Round != 7 || m[2].Round != 9 || m[2].ID != "f1.m3" {
				t.Fatalf("incorrect moment selection: %+v", m)
			}
			payload, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			wantExcess, wantPlace := "6.4", "apartments"
			if lang == i18n.RU {
				wantExcess, wantPlace = "6,4", "апсы"
			}
			if !strings.Contains(string(payload), `"excess":"`+wantExcess+`"`) || !strings.Contains(m[0].Detail, wantPlace) {
				t.Fatal("missing localized comparison or place in request")
			}
			for _, forbidden := range []string{"unselected-", "demo_gototick", "15962", "scoreboard", "tick_rate", "aim_botz_counter_strafe"} {
				if strings.Contains(string(payload), forbidden) {
					t.Errorf("unneeded %q in request", forbidden)
				}
			}
		})
	}
}

func allFragments(in modelInput) modelResponse {
	out := modelResponse{Task: in.Task}
	for _, item := range in.Items {
		ids := []string{item.Summary.ID}
		for _, m := range item.Moments {
			ids = append(ids, m.ID)
		}
		out.Items = append(out.Items, responseItem{FindingID: item.FindingID, FragmentIDs: ids})
	}
	return out
}

func TestFragmentContent(t *testing.T) {
	c := content.Default()
	for _, lang := range i18n.Langs {
		f := facts()
		// Two valid zones in different moments of the same finding.
		f.Findings[0].Evidence[1].Place = "Connector"
		r := newRenderer(f, lang, c)
		in := r.modelInput()
		response := allFragments(in)
		sections, err := in.validate(response)
		if err != nil || r.render(sections) != Render(f, lang, c) {
			t.Fatalf("%s: all references must reproduce the template: %v", lang, err)
		}
		response.Items[0].FragmentIDs = []string{"f1.summary", "f1.m2"}
		sections, err = in.validate(response)
		if err != nil || sections["f1"] != in.Items[0].Summary.line+"\n   · "+r.moment(f.Findings[0], f.Findings[0].Evidence[1]) {
			t.Fatalf("%s: selecting an episode must preserve its own clock, zone and command: %v", lang, err)
		}
		for name, change := range map[string]func(*modelResponse){
			"missing pick":                func(out *modelResponse) { out.Items = out.Items[1:] },
			"duplicate pick":              func(out *modelResponse) { out.Items[1] = out.Items[0] },
			"reordered picks":             func(out *modelResponse) { slices.Reverse(out.Items) },
			"related as standalone":       func(out *modelResponse) { out.Items[0].FindingID = "f3" },
			"foreign summary":             func(out *modelResponse) { out.Items[0].FragmentIDs[0] = "f2.summary" },
			"no fragments":                func(out *modelResponse) { out.Items[0].FragmentIDs = nil },
			"no evidence":                 func(out *modelResponse) { out.Items[0].FragmentIDs = []string{"f1.summary"} },
			"foreign moment":              func(out *modelResponse) { out.Items[0].FragmentIDs[1] = "f2.m1" },
			"repeated moment":             func(out *modelResponse) { out.Items[0].FragmentIDs[1] = "f1.m2" },
			"reordered moments":           func(out *modelResponse) { slices.Reverse(out.Items[0].FragmentIDs[1:]) },
			"unoffered fourth moment":     func(out *modelResponse) { out.Items[0].FragmentIDs[3] = "f1.m4" },
			"invented count":              func(out *modelResponse) { out.Items[0].FragmentIDs[0] = "99" },
			"number from another finding": func(out *modelResponse) { out.Items[0].FragmentIDs[1] = in.Items[1].Summary.Observed },
			"zone from another moment": func(out *modelResponse) {
				out.Items[0].FragmentIDs[1] = "f1.m1 " + c.Place(f.Match.Map, "Connector", lang)
			},
			"invented zone":          func(out *modelResponse) { out.Items[0].FragmentIDs[1] = "SecretPassage" },
			"invented outcome":       func(out *modelResponse) { out.Items[0].FragmentIDs[1] = "won the opening duel" },
			"missing task":           func(out *modelResponse) { out.Task = nil },
			"other drill in catalog": func(out *modelResponse) { out.Task = &reviewTask{"f1", "aim_botz_counter_strafe"} },
			"invented drill":         func(out *modelResponse) { out.Task = &reviewTask{"f1", "unknown"} },
			"wrong task finding":     func(out *modelResponse) { out.Task = &reviewTask{"f3", "demo_review"} },
		} {
			t.Run(string(lang)+"/"+name, func(t *testing.T) {
				out := allFragments(in)
				change(&out)
				if sections, err := in.validate(out); err == nil || sections != nil {
					t.Fatalf("invalid reply accepted, error %v", err)
				}
			})
		}
	}
}

func TestStrengthWithoutEvidenceOrTask(t *testing.T) {
	for _, lang := range i18n.Langs {
		f := facts()
		f.Review = findings.ReviewFacts{Strength: f.Review.Strength}
		f.Findings[3].Evidence = nil
		r := newRenderer(f, lang, content.Default())
		in := r.modelInput()
		reply := allFragments(in)
		sections, err := in.validate(reply)
		if err != nil || len(in.Items) != 1 || in.Task != nil || r.render(sections) != Render(f, lang, content.Default()) {
			t.Fatalf("%s: strength-only review differs from template: %v", lang, err)
		}
		reply.Task = &reviewTask{FindingID: "f4", DrillID: "demo_review"}
		if _, err := in.validate(reply); err == nil {
			t.Fatal("accepted unsolicited task")
		}
	}
}
