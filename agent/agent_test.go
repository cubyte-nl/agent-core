package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cubyte-nl/agent-core/llm"
	"github.com/cubyte-nl/agent-core/tools"
)

func TestRunWithProgressHeartbeat(t *testing.T) {
	var lines []string
	say := func(s string) { lines = append(lines, s) }
	want := tools.Result{Tool: "nuclei", Allowed: true}
	got := runWithProgress(say, 10*time.Millisecond, "nuclei", func() tools.Result {
		time.Sleep(55 * time.Millisecond)
		return want
	})
	if got.Tool != want.Tool || !got.Allowed {
		t.Fatalf("tool result not passed through: %+v", got)
	}
	if len(lines) == 0 {
		t.Fatal("a slow tool should emit at least one heartbeat")
	}
	for _, l := range lines {
		if !strings.Contains(l, "nuclei still running") {
			t.Errorf("unexpected heartbeat line: %q", l)
		}
	}
}

func TestRunWithProgressFastToolSilent(t *testing.T) {
	n := 0
	say := func(string) { n++ }
	runWithProgress(say, 10*time.Millisecond, "dns", func() tools.Result {
		return tools.Result{Tool: "dns", Allowed: true}
	})
	if n != 0 {
		t.Errorf("a fast tool should emit no heartbeat, got %d line(s)", n)
	}
}

func TestRunWithProgressDisabled(t *testing.T) {
	n := 0
	say := func(string) { n++ }
	runWithProgress(say, -1, "nuclei", func() tools.Result {
		time.Sleep(30 * time.Millisecond)
		return tools.Result{Tool: "nuclei", Allowed: true}
	})
	if n != 0 {
		t.Errorf("disabled heartbeat should emit nothing, got %d line(s)", n)
	}
}

func TestCapToolResult(t *testing.T) {
	small := `{"tool":"dns","allowed":true}`
	if got := capToolResult(small, 8<<10); got != small {
		t.Errorf("content within the cap should be unchanged, got %q", got)
	}
	if got := capToolResult(small, -1); got != small {
		t.Errorf("a negative cap should leave content uncapped, got %q", got)
	}

	big := strings.Repeat("x", 100)
	got := capToolResult(big, 40)
	if len(got) <= 40 || !strings.HasPrefix(got, strings.Repeat("x", 40)) {
		t.Errorf("over-cap content should keep the first 40 bytes then a note, got %q", got)
	}
	if !strings.Contains(got, "truncated 60 bytes") {
		t.Errorf("truncation note should report the dropped byte count, got %q", got)
	}
}

func TestCapToolResultTrimsSplitRune(t *testing.T) {
	// A multi-byte rune straddling the cut must not leave an invalid fragment.
	s := strings.Repeat("a", 10) + "€" // '€' is 3 bytes
	got := capToolResult(s, 11)        // cut lands inside the euro sign
	if !utf8.ValidString(got) {
		t.Errorf("capped content must stay valid UTF-8, got %q", got)
	}
}

func TestRunTurnStateInjectedAndReplaced(t *testing.T) {
	// The model calls a tool twice, then writes the final report. Every chat
	// request is captured so the injected state message can be inspected.
	type captured struct {
		contents []string
	}
	var reqs []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
			Tools []any `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		c := captured{}
		for _, m := range req.Messages {
			c.contents = append(c.contents, m.Content)
		}
		reqs = append(reqs, c)
		msg := map[string]any{"role": "assistant", "content": "the final report"}
		if len(req.Tools) > 0 {
			msg = map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{
				"id": "1", "type": "function", "function": map[string]any{"name": "sleep", "arguments": "{}"},
			}}}
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}})
	}))
	defer srv.Close()

	state := "STATE-v1"
	_, err := Run(context.Background(), testBoundary{}, llm.NewClient(srv.URL, "", "m"),
		tools.NewRegistry[testBoundary](false, sleepTool{}), Config[testBoundary]{
			MaxSteps:     10,
			SystemPrompt: func(testBoundary, string, string) string { return "system" },
			RenderReport: func(_ testBoundary, _, final string, _ int, _ string) string { return final },
			RenderTurnState: func(turn, _ int) string {
				if turn == 0 {
					return state
				}
				return "STATE-v2"
			},
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) < 3 {
		t.Fatalf("want at least 3 chat requests (2 tool turns + final), got %d", len(reqs))
	}
	// Every request carries exactly one state message: injected once, then
	// replaced in place — never a second, stale copy.
	for i, c := range reqs {
		var states []string
		for _, m := range c.contents {
			if strings.HasPrefix(m, "STATE-") {
				states = append(states, m)
			}
		}
		if len(states) != 1 {
			t.Errorf("request %d: want exactly one state message, got %d: %v", i+1, len(states), states)
		}
	}
	if !strings.HasPrefix(reqs[0].contents[2], "STATE-v1") {
		t.Errorf("first request should carry the v1 state right after the objective, got %q", reqs[0].contents[2])
	}
	if !strings.HasPrefix(reqs[2].contents[2], "STATE-v2") {
		t.Errorf("later requests should carry the replaced v2 state at the same position, got %q", reqs[2].contents[2])
	}
}

type testBoundary struct{}

func (testBoundary) Describe() string { return "test" }

type sleepTool struct{}

func (sleepTool) Name() string               { return "sleep" }
func (sleepTool) Description() string        { return "sleeps briefly" }
func (sleepTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (sleepTool) Mutating() bool             { return false }
func (sleepTool) Run(context.Context, testBoundary, map[string]any) tools.Result {
	time.Sleep(20 * time.Millisecond)
	return tools.Result{Tool: "sleep", Allowed: true}
}

func TestRunTimeBudget(t *testing.T) {
	// A model that keeps calling a tool, and writes a report once it is asked
	// without tools (the forced final turn).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []any `json:"tools"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		msg := map[string]any{"role": "assistant", "content": "the final report"}
		if len(req.Tools) > 0 {
			msg = map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{
				"id": "1", "type": "function", "function": map[string]any{"name": "sleep", "arguments": "{}"},
			}}}
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": msg, "finish_reason": "stop"}}})
	}))
	defer srv.Close()

	var lines []string
	res, err := Run(context.Background(), testBoundary{}, llm.NewClient(srv.URL, "", "m"),
		tools.NewRegistry[testBoundary](false, sleepTool{}), Config[testBoundary]{
			MaxDuration:  100 * time.Millisecond,
			Out:          func(s string) { lines = append(lines, s) },
			SystemPrompt: func(testBoundary, string, string) string { return "system" },
			RenderReport: func(_ testBoundary, _, final string, _ int, _ string) string { return final },
		})
	if err != nil {
		t.Fatal(err)
	}
	if res.Report != "the final report" {
		t.Errorf("report = %q, want the forced final report", res.Report)
	}
	if res.Steps == 0 || res.Steps >= 100 {
		t.Errorf("steps = %d, want the time budget to stop the run early", res.Steps)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "time budget reached") {
		t.Errorf("no time-budget line in the action log: %q", lines)
	}
}
