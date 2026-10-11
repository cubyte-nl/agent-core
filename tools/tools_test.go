package tools_test

import (
	"context"
	"testing"

	"github.com/cubyte-nl/agent-core/coretest"
	"github.com/cubyte-nl/agent-core/tools"
)

var ctx = context.Background()

// allowList is a minimal fail-closed boundary used across the core tests: it
// permits only the targets it was told about. Copy this shape when writing a
// real boundary — the default answer is always "no".
type allowList struct{ allowed map[string]bool }

func (a allowList) check(target string) (bool, string) {
	if a.allowed[target] {
		return true, "allow-listed"
	}
	return false, "not in allow-list"
}

func (allowList) Describe() string { return "only allow-listed targets permitted" }

// echo is a read-only tool; write is a mutating one. Together they exercise
// the registry's mutating-drop behaviour.
type echo struct{}

func (echo) Name() string               { return "echo" }
func (echo) Description() string        { return "repeat text" }
func (echo) Mutating() bool             { return false }
func (echo) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (echo) Run(_ context.Context, b allowList, args map[string]any) tools.Result {
	text := tools.StrArg(args, "text")
	if ok, reason := b.check(text); !ok {
		return tools.Result{Tool: "echo", Target: text, Allowed: false, Reason: reason}
	}
	return tools.Result{Tool: "echo", Target: text, Allowed: true, Payload: map[string]any{"output": text}}
}

type write struct{}

func (write) Name() string               { return "write" }
func (write) Description() string        { return "mutating example" }
func (write) Mutating() bool             { return true }
func (write) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (write) Run(context.Context, allowList, map[string]any) tools.Result {
	return tools.Result{Tool: "write", Allowed: true}
}

func TestRegistryConformance(t *testing.T) {
	ro := tools.NewRegistry[allowList](false, echo{}, write{})
	rw := tools.NewRegistry[allowList](true, echo{}, write{})
	coretest.AssertRegistryDropsMutating(t, ro, rw, 1, 2)

	// A mutating tool is unreachable in a read-only registry.
	if res := ro.Run(ctx, allowList{}, "write", "{}"); res.Allowed {
		t.Error("mutating tool must be unreachable in a read-only registry")
	}
}

func TestRegistryRunUnknownTool(t *testing.T) {
	r := tools.NewRegistry[allowList](false, echo{})
	if res := r.Run(ctx, allowList{}, "nope", "{}"); res.Allowed {
		t.Error("unknown tool must be denied")
	}
}

func TestRegistryRunInvalidJSON(t *testing.T) {
	r := tools.NewRegistry[allowList](false, echo{})
	if res := r.Run(ctx, allowList{allowed: map[string]bool{"hi": true}}, "echo", "{not json"); res.Allowed {
		t.Error("malformed arguments must be reported back, not executed")
	}
}

func TestBoundaryContract(t *testing.T) {
	b := allowList{allowed: map[string]bool{"host-a": true}}
	coretest.AssertFailClosed(t, b.check,
		[]string{"host-a"},
		[]string{"host-b", "", "HOST-A"})

	r := tools.NewRegistry[allowList](false, echo{})
	coretest.AssertDenialPropagates(t, r.Run(ctx, allowList{}, "echo", `{"text":"host-x"}`))
}
