package telegram

import (
	"cmp"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"
)

const testToken = "123456:TEST-token-not-real"

type apiCall struct {
	method string
	params map[string]string
}

// fakeAPI is a minimal Bot API server. It records calls, serves queued
// updates and keeps the command menu and descriptions per language code.
type fakeAPI struct {
	*httptest.Server
	t *testing.T

	mu                sync.Mutex
	calls             []apiCall
	updates           []*models.Update
	commands          map[string]string // language code -> commands as JSON
	descriptions      map[string]string
	shortDescriptions map[string]string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{
		t:                 t,
		commands:          map[string]string{},
		descriptions:      map[string]string{},
		shortDescriptions: map[string]string{},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	method, ok := strings.CutPrefix(r.URL.Path, "/bot"+testToken+"/")
	if !ok {
		writeJSON(w, map[string]any{"ok": false, "error_code": 401, "description": "Unauthorized"})
		return
	}
	params := map[string]string{}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for k, v := range r.MultipartForm.Value {
			params[k] = v[0]
		}
	}

	result, ok := f.result(method, params)
	if !ok {
		f.t.Errorf("unexpected Bot API method %s", method)
		writeJSON(w, map[string]any{"ok": false, "error_code": 404, "description": "Not Found"})
		return
	}
	if updates, isUpdates := result.([]*models.Update); isUpdates && len(updates) == 0 {
		// Stand in for long polling without spinning.
		select {
		case <-r.Context().Done():
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	writeJSON(w, map[string]any{"ok": true, "result": result})
}

func (f *fakeAPI) result(method string, params map[string]string) (any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, apiCall{method: method, params: params})

	code := params["language_code"]
	switch method {
	case "getMe":
		return models.User{ID: 123456, IsBot: true, FirstName: "Fragmentor", Username: "fragmentor_ai_bot"}, true
	case "getUpdates":
		updates := f.updates
		f.updates = nil
		if updates == nil {
			updates = []*models.Update{}
		}
		return updates, true
	case "sendMessage":
		chatID, _ := strconv.ParseInt(params["chat_id"], 10, 64)
		return map[string]any{
			"message_id": len(f.calls),
			"date":       0,
			"chat":       map[string]any{"id": chatID, "type": "private"},
			"text":       params["text"],
		}, true
	case "editMessageText":
		chatID, _ := strconv.ParseInt(params["chat_id"], 10, 64)
		messageID, _ := strconv.Atoi(params["message_id"])
		return map[string]any{
			"message_id": messageID,
			"date":       0,
			"chat":       map[string]any{"id": chatID, "type": "private"},
			"text":       params["text"],
		}, true
	case "answerCallbackQuery", "sendChatAction":
		return true, true
	case "getMyCommands":
		return json.RawMessage(cmp.Or(f.commands[code], "[]")), true
	case "setMyCommands":
		f.commands[code] = params["commands"]
		return true, true
	case "getMyDescription":
		return models.BotDescription{Description: f.descriptions[code]}, true
	case "setMyDescription":
		f.descriptions[code] = params["description"]
		return true, true
	case "getMyShortDescription":
		return models.BotShortDescription{ShortDescription: f.shortDescriptions[code]}, true
	case "setMyShortDescription":
		f.shortDescriptions[code] = params["short_description"]
		return true, true
	}
	return nil, false
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeAPI) queueUpdates(updates ...*models.Update) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, updates...)
}

// callsTo returns the recorded calls of method.
func (f *fakeAPI) callsTo(method string) []apiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []apiCall
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// methods returns the names of all recorded calls in order.
func (f *fakeAPI) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.method
	}
	return out
}

// sentTexts returns the texts of the sent messages in order.
func (f *fakeAPI) sentTexts() []string {
	var out []string
	for _, c := range f.callsTo("sendMessage") {
		out = append(out, c.params["text"])
	}
	return out
}

// editedTexts returns the texts messages were edited to, in order.
func (f *fakeAPI) editedTexts() []string {
	var out []string
	for _, c := range f.callsTo("editMessageText") {
		out = append(out, c.params["text"])
	}
	return out
}
