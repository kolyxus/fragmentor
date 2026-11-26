package review

import (
	"reflect"
	"testing"
)

func TestDecodeResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want modelResponse
	}{
		{"empty", `{"items":[]}`, modelResponse{Items: []responseItem{}}},
		{"references", ` {"task":{"drill_id":"demo_review","finding_id":"f1"},"items":[{"fragment_ids":["f1.summary","f1.e1"],"finding_id":"f1"}]} `,
			modelResponse{
				Items: []responseItem{{FindingID: "f1", FragmentIDs: []string{"f1.summary", "f1.e1"}}},
				Task:  &reviewTask{FindingID: "f1", DrillID: "demo_review"},
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeResponse(tc.raw)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("decodeResponse() = %#v, %v; want %#v", got, err, tc.want)
			}
		})
	}
}

func TestDecodeResponseRejectsInvalidShape(t *testing.T) {
	for name, raw := range map[string]string{
		"malformed":          `{"items":[}`,
		"trailing object":    `{"items":[]} {}`,
		"markdown":           "~~~json\n{\"items\":[]}\n~~~",
		"null root":          `null`,
		"root array":         `[]`,
		"missing items":      `{}`,
		"null items":         `{"items":null}`,
		"unknown field":      `{"items":[],"score":4}`,
		"case alias":         `{"Items":[]}`,
		"duplicate field":    `{"items":[],"items":[]}`,
		"escaped duplicate":  `{"items":[],"\u0069tems":[]}`,
		"invalid utf8":       "{\"items\":[{\"finding_id\":\"\xff\",\"fragment_ids\":[]}]}",
		"unpaired surrogate": `{"items":[{"finding_id":"\ud800","fragment_ids":[]}]}`,
		"null item":          `{"items":[null]}`,
		"missing finding":    `{"items":[{"fragment_ids":[]}]}`,
		"empty finding":      `{"items":[{"finding_id":" ","fragment_ids":[]}]}`,
		"null finding":       `{"items":[{"finding_id":null,"fragment_ids":[]}]}`,
		"missing fragments":  `{"items":[{"finding_id":"f1"}]}`,
		"null fragments":     `{"items":[{"finding_id":"f1","fragment_ids":null}]}`,
		"numeric fragment":   `{"items":[{"finding_id":"f1","fragment_ids":[12]}]}`,
		"null fragment":      `{"items":[{"finding_id":"f1","fragment_ids":[null]}]}`,
		"empty fragment":     `{"items":[{"finding_id":"f1","fragment_ids":[" "]}]}`,
		"unrequested text":   `{"items":[{"finding_id":"f1","fragment_ids":[],"text":"won the duel"}]}`,
		"duplicate finding":  `{"items":[{"finding_id":"f1","finding_id":"f2","fragment_ids":[]}]}`,
		"null task":          `{"items":[],"task":null}`,
		"missing drill":      `{"items":[],"task":{"finding_id":"f1"}}`,
		"missing target":     `{"items":[],"task":{"drill_id":"demo_review"}}`,
		"empty target":       `{"items":[],"task":{"finding_id":"","drill_id":"demo_review"}}`,
		"unknown task field": `{"items":[],"task":{"finding_id":"f1","drill_id":"demo_review","extra":true}}`,
		"duplicate drill":    `{"items":[],"task":{"finding_id":"f1","drill_id":"a","drill_id":"b"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeResponse(raw); err == nil {
				t.Fatalf("accepted %q", raw)
			}
		})
	}
}
