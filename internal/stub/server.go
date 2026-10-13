package stub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxBody caps request bodies. Real requests from the runtime are well under this.
const maxBody = 8 << 20

// Server serves the OpenAI-compatible endpoints the runtime calls.
type Server struct {
	// Log, if set, receives one JSON line per completion request.
	Log io.Writer

	mu  sync.Mutex
	seq atomic.Int64
}

// ServeHTTP routes /v1/models and /v1/chat/completions.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/models"):
		writeJSON(w, map[string]any{
			"object": "list",
			"data":   []map[string]any{{"id": "scripted", "object": "model"}},
		})
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/chat/completions"):
		s.completions(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) completions(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	action := Respond(req)
	s.log(req, action)

	id := fmt.Sprintf("stub-%d", s.seq.Add(1))
	if req.Stream {
		s.stream(w, id, action, req.StreamOptions != nil && req.StreamOptions.IncludeUsage)
		return
	}
	msg := map[string]any{"role": "assistant", "content": nil}
	finish := "stop"
	if action.IsToolCall() {
		msg["tool_calls"] = []ToolCall{toolCall(id, action, nil)}
		finish = "tool_calls"
	} else {
		msg["content"] = action.Text
	}
	writeJSON(w, map[string]any{
		"id": id, "object": "chat.completion", "created": time.Now().Unix(), "model": "scripted",
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finish}},
		"usage":   Usage{},
	})
}

func (s *Server) stream(w http.ResponseWriter, id string, action Action, includeUsage bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	created := time.Now().Unix()

	emit := func(chunk map[string]any) {
		chunk["id"], chunk["object"], chunk["created"], chunk["model"] = id, "chat.completion.chunk", created, "scripted"
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	choice := func(delta map[string]any, finish any) map[string]any {
		return map[string]any{"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}}}
	}

	emit(choice(map[string]any{"role": "assistant", "content": ""}, nil))
	if action.IsToolCall() {
		zero := 0
		emit(choice(map[string]any{"tool_calls": []ToolCall{toolCall(id, action, &zero)}}, nil))
		emit(choice(map[string]any{}, "tool_calls"))
	} else {
		emit(choice(map[string]any{"content": action.Text}, nil))
		emit(choice(map[string]any{}, "stop"))
	}
	if includeUsage {
		emit(map[string]any{"choices": []any{}, "usage": Usage{}})
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func toolCall(id string, a Action, index *int) ToolCall {
	args, _ := json.Marshal(a.Args)
	return ToolCall{Index: index, ID: "call_" + id, Type: "function",
		Function: FunctionCall{Name: a.Tool, Arguments: string(args)}}
}

func (s *Server) log(req ChatRequest, a Action) {
	if s.Log == nil {
		return
	}
	line, _ := json.Marshal(map[string]any{
		"time": time.Now().UTC().Format(time.RFC3339Nano), "messages": len(req.Messages), "action": a,
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(s.Log, "%s\n", line)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
