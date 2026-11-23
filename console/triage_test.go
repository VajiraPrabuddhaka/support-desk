package main

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
)

func dataPart(kind string, data map[string]any) *a2a.Part {
	p := a2a.NewDataPart(data)
	p.SetMeta("adk_type", kind)
	return p
}

func TestAddPartsBuildsTheTimeline(t *testing.T) {
	j := &job{jobView: jobView{State: "running", Steps: []*step{}}}

	j.addParts(a2a.ContentParts{dataPart("function_call", map[string]any{
		"id": "c1", "name": "issue_read", "args": map[string]any{"method": "get", "issue_number": float64(1)},
	})})
	j.addParts(a2a.ContentParts{dataPart("function_response", map[string]any{
		"id": "c1", "name": "issue_read", "response": map[string]any{"output": "{}"},
	})})
	j.addParts(a2a.ContentParts{dataPart("function_call", map[string]any{
		"id": "c2", "name": "billing_agent", "args": map[string]any{"request": "Account ACC-1042 ..."},
	})})
	j.addParts(a2a.ContentParts{dataPart("function_response", map[string]any{
		"id": "c2", "name": "billing_agent", "response": map[string]any{"result": "Account: Bluefin, verified"},
	})})
	j.addParts(a2a.ContentParts{dataPart("function_call", map[string]any{
		"id": "c3", "name": "issue_write",
		"args": map[string]any{"method": "add_labels", "issue_number": float64(1), "labels": []any{"type/billing", "priority/P2"}},
	})})
	j.addParts(a2a.ContentParts{dataPart("function_response", map[string]any{
		"id": "c3", "name": "issue_write", "response": map[string]any{"error": "refused by policy"},
	})})
	j.addParts(a2a.ContentParts{a2a.NewTextPart("#1 triaged.")})
	j.finish()

	want := []struct{ kind, title, state string }{
		{"tool", "Read ticket #1", "done"},
		{"handoff", "Asked billing-agent over A2A", "done"},
		{"reply", "billing-agent answered", "done"},
		{"tool", "Labelled #1", "failed"},
		{"say", "support-triage", "done"},
	}
	v := j.snapshot()
	if len(v.Steps) != len(want) {
		t.Fatalf("got %d steps, want %d: %+v", len(v.Steps), len(want), v.Steps)
	}
	for i, w := range want {
		s := v.Steps[i]
		if s.Kind != w.kind || s.Title != w.title || s.State != w.state {
			t.Errorf("step %d = %s/%q/%s, want %s/%q/%s", i, s.Kind, s.Title, s.State, w.kind, w.title, w.state)
		}
	}
	if v.Steps[2].Detail != "Account: Bluefin, verified" {
		t.Errorf("billing answer = %q", v.Steps[2].Detail)
	}
	if v.Steps[3].Detail != "type/billing,priority/P2" || v.Steps[3].Result != "refused by policy" {
		t.Errorf("labels step = %+v", v.Steps[3])
	}
	if v.Summary != "#1 triaged." || v.State != "done" {
		t.Errorf("summary %q, state %q", v.Summary, v.State)
	}
}

// An agent on kagent labels its parts with kagent's keys.
func TestAddPartsReadsKagentLabels(t *testing.T) {
	kagentPart := func(kind string, data map[string]any) *a2a.Part {
		p := a2a.NewDataPart(data)
		p.SetMeta("kagent.dev/a2a/part-type", kind)
		return p
	}
	thought := a2a.NewTextPart("Let me read the ticket first.")
	thought.SetMeta("kagent.dev/a2a/thought", true)

	j := &job{jobView: jobView{State: "running", Steps: []*step{}}}
	j.addParts(a2a.ContentParts{thought})
	j.addParts(a2a.ContentParts{kagentPart("function_call", map[string]any{
		"id": "c1", "name": "billing_agent", "args": map[string]any{"request": "Account ACC-1042 ..."},
	})})
	j.addParts(a2a.ContentParts{kagentPart("function_response", map[string]any{
		"id": "c1", "name": "billing_agent", "response": map[string]any{"result": "Account: Bluefin, verified", "subagent": "billing_agent"},
	})})
	j.addParts(a2a.ContentParts{a2a.NewTextPart("#1 triaged.")})
	j.finish()

	want := []struct{ kind, title, state string }{
		{"handoff", "Asked billing-agent over A2A", "done"},
		{"reply", "billing-agent answered", "done"},
		{"say", "support-triage", "done"},
	}
	v := j.snapshot()
	if len(v.Steps) != len(want) {
		t.Fatalf("got %d steps, want %d: %+v", len(v.Steps), len(want), v.Steps)
	}
	for i, w := range want {
		s := v.Steps[i]
		if s.Kind != w.kind || s.Title != w.title || s.State != w.state {
			t.Errorf("step %d = %s/%q/%s, want %s/%q/%s", i, s.Kind, s.Title, s.State, w.kind, w.title, w.state)
		}
	}
	if v.Steps[1].Detail != "Account: Bluefin, verified" || v.Summary != "#1 triaged." {
		t.Errorf("answer %q, summary %q", v.Steps[1].Detail, v.Summary)
	}
}

func TestFriendlyError(t *testing.T) {
	if got := friendlyError("OpenAI API error: 429 Too Many Requests"); got == "OpenAI API error: 429 Too Many Requests" {
		t.Error("a 429 gets no hint")
	}
	if got := friendlyError(""); got == "" {
		t.Error("an empty error stays empty")
	}
}
