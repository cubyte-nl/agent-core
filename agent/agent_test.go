package agent

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
