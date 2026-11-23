package kit

import (
	"encoding/json"
	"log/slog"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/tool"
)

// ToolLogging returns callbacks that log every tool call and its result, so the
// worker pod's log shows what each agent does.
func ToolLogging() (llmagent.BeforeToolCallback, llmagent.AfterToolCallback) {
	before := func(_ agent.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
		slog.Info("tool call", "tool", t.Name(), "args", compact(args, 600))
		return nil, nil
	}
	after := func(_ agent.Context, t tool.Tool, _, result map[string]any, err error) (map[string]any, error) {
		if err != nil {
			slog.Warn("tool failed", "tool", t.Name(), "error", err)
			return nil, nil
		}
		slog.Info("tool result", "tool", t.Name(), "result", compact(result, 400))
		return nil, nil
	}
	return before, after
}

// compact renders v as JSON, cut to at most limit bytes.
func compact(v any, limit int) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<unprintable>"
	}
	if len(b) > limit {
		return string(b[:limit]) + "…"
	}
	return string(b)
}
