package stub

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func post(t *testing.T, h http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(b)))
	return rec
}

func leadRequest(stream bool) map[string]any {
	return map[string]any{
		"model":  "scripted",
		"stream": stream,
		"messages": []map[string]any{
			{"role": "system", "content": "ROLE: lead"},
			{"role": "user", "content": "Is target/ ready to ship?"},
		},
		"stream_options": map[string]any{"include_usage": true},
	}
}

func TestModels(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"scripted"`) {
		t.Fatalf("GET /v1/models = %d %s", rec.Code, rec.Body)
	}
}

func TestUnknownRoute(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/embeddings", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestBadJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Server{}).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader("{")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestNonStreamingToolCall(t *testing.T) {
	rec := post(t, &Server{}, leadRequest(false))
	var resp struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v\n%s", err, rec.Body)
	}
	c := resp.Choices[0]
	if c.FinishReason != "tool_calls" || len(c.Message.ToolCalls) != 1 {
		t.Fatalf("unexpected choice: %+v", c)
	}
	fn := c.Message.ToolCalls[0].Function
	var args map[string]string
	if err := json.Unmarshal([]byte(fn.Arguments), &args); err != nil {
		t.Fatalf("arguments are not JSON: %v", err)
	}
	if fn.Name != "transfer_task" || args["agent"] != "container_auditor" {
		t.Fatalf("got %s(%v)", fn.Name, args)
	}
}

func TestStreamingToolCall(t *testing.T) {
	var logBuf bytes.Buffer
	rec := post(t, &Server{Log: &logBuf}, leadRequest(true))
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}

	var chunks []map[string]any
	done := false
	sc := bufio.NewScanner(rec.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			done = true
			continue
		}
		var c map[string]any
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			t.Fatalf("bad chunk %q: %v", data, err)
		}
		chunks = append(chunks, c)
	}
	if !done {
		t.Fatal("stream did not end with [DONE]")
	}
	body := rec.Body.String()
	for _, want := range []string{`"transfer_task"`, `"finish_reason":"tool_calls"`, `"usage"`} {
		if !strings.Contains(body, want) && !containsChunk(chunks, want) {
			t.Fatalf("stream missing %s", want)
		}
	}
	if !strings.Contains(logBuf.String(), "transfer_task") {
		t.Fatalf("request log missing action: %q", logBuf.String())
	}
}

func TestStreamingText(t *testing.T) {
	req := leadRequest(true)
	req["messages"] = []map[string]any{{"role": "system", "content": "title please"}, {"role": "user", "content": "x"}}
	rec := post(t, &Server{}, req)
	if !strings.Contains(rec.Body.String(), FallbackText) || !strings.Contains(rec.Body.String(), `"finish_reason":"stop"`) {
		t.Fatalf("unexpected stream: %s", rec.Body)
	}
}

func containsChunk(chunks []map[string]any, s string) bool {
	for _, c := range chunks {
		b, _ := json.Marshal(c)
		if strings.Contains(string(b), strings.Trim(s, `"`)) {
			return true
		}
	}
	return false
}
