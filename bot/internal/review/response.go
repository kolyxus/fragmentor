package review

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strings"
)

type modelResponse struct {
	Items []responseItem `json:"items"`
	Task  *reviewTask    `json:"task,omitempty"`
}

type responseItem struct {
	FindingID   string   `json:"finding_id"`
	FragmentIDs []string `json:"fragment_ids"`
}

// decodeResponse checks the wire format independently of the supplied findings.
func decodeResponse(raw string) (modelResponse, error) {
	// Keep task as raw JSON to distinguish an omitted task from an explicit null.
	var wire struct {
		Items []responseItem `json:"items"`
		Task  jsontext.Value `json:"task"`
	}
	strict := json.RejectUnknownMembers(true)
	if err := json.Unmarshal([]byte(raw), &wire, strict); err != nil {
		return modelResponse{}, errors.New("review: invalid response JSON or fields")
	}
	if wire.Items == nil {
		return modelResponse{}, errors.New("review: items array is required")
	}
	for _, item := range wire.Items {
		if strings.TrimSpace(item.FindingID) == "" || item.FragmentIDs == nil {
			return modelResponse{}, errors.New("review: item requires finding_id and fragment_ids")
		}
		for _, id := range item.FragmentIDs {
			if strings.TrimSpace(id) == "" {
				return modelResponse{}, errors.New("review: empty fragment ID")
			}
		}
	}
	out := modelResponse{Items: wire.Items}
	if wire.Task != nil {
		var task reviewTask
		if err := json.Unmarshal(wire.Task, &task, strict); err != nil ||
			strings.TrimSpace(task.FindingID) == "" || strings.TrimSpace(task.DrillID) == "" {
			return modelResponse{}, errors.New("review: task requires finding_id and drill_id")
		}
		out.Task = &task
	}
	return out, nil
}
