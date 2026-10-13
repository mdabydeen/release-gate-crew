package stub

import (
	"encoding/json"
	"strings"
	"testing"
)

func text(role, s string) Message {
	b, _ := json.Marshal(s)
	return Message{Role: role, Content: b}
}

func parts(role string, ss ...string) Message {
	p := make([]map[string]string, len(ss))
	for i, s := range ss {
		p[i] = map[string]string{"type": "text", "text": s}
	}
	b, _ := json.Marshal(p)
	return Message{Role: role, Content: b}
}

func TestMessageText(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want string
	}{
		{"string", text("user", "hi"), "hi"},
		{"parts", parts("system", "a", "b"), "a\nb"},
		{"null", Message{Role: "assistant", Content: json.RawMessage("null")}, ""},
		{"empty", Message{Role: "assistant"}, ""},
		{"garbage", Message{Role: "user", Content: json.RawMessage(`{"x":1}`)}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.Text(); got != tt.want {
				t.Fatalf("Text() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRespondFallsBackWithoutRole(t *testing.T) {
	got := Respond(ChatRequest{Messages: []Message{text("system", "Generate a title"), text("user", "x")}})
	if got.IsToolCall() || got.Text != FallbackText {
		t.Fatalf("got %+v, want fallback text", got)
	}
}

func TestRespondUnknownRoleFallsBack(t *testing.T) {
	got := Respond(ChatRequest{Messages: []Message{text("system", "ROLE: stranger"), text("user", "x")}})
	if got.Text != FallbackText {
		t.Fatalf("got %+v, want fallback", got)
	}
}

// Each script must walk its documented sequence, one step per tool result,
// and finish with a labelled text answer.
func TestScriptSequences(t *testing.T) {
	tests := []struct {
		role  string
		tools []string
	}{
		{"lead", []string{"transfer_task", "transfer_task", "transfer_task"}},
		{"container_auditor", []string{"read_multiple_files", "shell", "read_file", "shell"}},
		{"pipeline_auditor", []string{"read_file", "read_file"}},
		{"scribe", []string{"write_file", "write_file"}},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			msgs := []Message{parts("system", "ROLE: "+tt.role+"\n\nYou are..."), text("user", "Please proceed.")}
			for step, wantTool := range tt.tools {
				got := Respond(ChatRequest{Messages: msgs})
				if got.Tool != wantTool {
					t.Fatalf("step %d: tool = %q, want %q", step, got.Tool, wantTool)
				}
				msgs = append(msgs, Message{Role: "assistant"}, text("tool", "result FROM pull_request_target"))
			}
			final := Respond(ChatRequest{Messages: msgs})
			if final.IsToolCall() {
				t.Fatalf("final step: got tool call %q, want text", final.Tool)
			}
			if !strings.HasPrefix(final.Text, StubLabel) {
				t.Fatalf("final text %q is not labelled %q", final.Text, StubLabel)
			}
		})
	}
}

func TestLeadDelegatesInOrder(t *testing.T) {
	msgs := []Message{text("system", "ROLE: lead"), text("user", "Is target/ ready to ship?")}
	var agents []string
	for i := 0; i < 3; i++ {
		a := Respond(ChatRequest{Messages: msgs})
		agents = append(agents, a.Args["agent"].(string))
		msgs = append(msgs, Message{Role: "assistant"}, text("tool", "findings "+a.Args["agent"].(string)))
	}
	want := []string{"container_auditor", "pipeline_auditor", "scribe"}
	if strings.Join(agents, ",") != strings.Join(want, ",") {
		t.Fatalf("delegation order = %v, want %v", agents, want)
	}
}

func TestLeadForwardsFindingsToScribe(t *testing.T) {
	msgs := []Message{text("system", "ROLE: lead"), text("user", "go"),
		{Role: "assistant"}, text("tool", "CONTAINER-FINDINGS"),
		{Role: "assistant"}, text("tool", "PIPELINE-FINDINGS")}
	a := Respond(ChatRequest{Messages: msgs})
	task, _ := a.Args["task"].(string)
	if !strings.Contains(task, "CONTAINER-FINDINGS") || !strings.Contains(task, "PIPELINE-FINDINGS") {
		t.Fatalf("scribe task does not carry both findings: %q", task)
	}
}

// transfer_task puts the task in a <task> block of a system message; the
// scribe must write those findings, not the "Please proceed." user turn.
func TestScribeReadsDelegatedTask(t *testing.T) {
	sys := "ROLE: scribe\n...\n<task>\n" + ScribeTaskPrefix + "\n\n## Container\nX-FINDING\n</task>"
	a := Respond(ChatRequest{Messages: []Message{text("system", sys), text("user", "Please proceed.")}})
	if a.Tool != "write_file" || a.Args["path"] != "reports/gate-report.md" {
		t.Fatalf("got %+v, want write_file reports/gate-report.md", a)
	}
	content := a.Args["content"].(string)
	if !strings.Contains(content, "X-FINDING") || strings.Contains(content, "Please proceed") {
		t.Fatalf("report content wrong: %q", content)
	}
}

// Only tool results after the latest user message count as steps, so a new
// question restarts the script.
func TestStepsResetOnNewUserMessage(t *testing.T) {
	msgs := []Message{text("system", "ROLE: pipeline_auditor"), text("user", "first"),
		{Role: "assistant"}, text("tool", "r1"), text("user", "second")}
	if a := Respond(ChatRequest{Messages: msgs}); a.Tool != "read_file" || a.Args["path"] != "target/ci/build.yml" {
		t.Fatalf("got %+v, want first step", a)
	}
}

func TestClip(t *testing.T) {
	if got := clip("  abc  ", 10); got != "abc" {
		t.Fatalf("clip short = %q", got)
	}
	if got := clip("abcdef", 3); got != "abc ..." {
		t.Fatalf("clip long = %q", got)
	}
}
