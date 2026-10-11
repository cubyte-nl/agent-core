// Package coretest provides the conformance checks every agent built on the
// core is expected to satisfy. An agent's own test file wires its concrete
// boundary and tools into these helpers so the shared safety contracts —
// fail-closed boundary, read-only runs never exposing a mutating tool — are
// re-verified per agent rather than trusted by convention.
package coretest

import (
	"strings"
	"testing"

	"github.com/cubyte-nl/agent-core/tools"
)

// Checker is the fail-closed check a concrete boundary exposes for testing: it
// reports whether a target is allowed. An agent adapts its own boundary
// (collie's CheckHost, mole's CheckTarget, ...) to this shape.
type Checker func(target string) (allowed bool, reason string)

// AssertFailClosed verifies a boundary allows the wantAllowed targets and
// denies everything in wantDenied (which should include the empty target). It
// is the contract every real boundary must meet: the default answer is no.
func AssertFailClosed(t *testing.T, check Checker, wantAllowed, wantDenied []string) {
	t.Helper()
	for _, target := range wantAllowed {
		if ok, reason := check(target); !ok {
			t.Errorf("target %q should be allowed, denied: %s", target, reason)
		}
	}
	for _, target := range wantDenied {
		if ok, _ := check(target); ok {
			t.Errorf("target %q should be denied by a fail-closed boundary, got allowed", target)
		}
	}
}

// AssertRegistryDropsMutating verifies that a read-only registry does not
// expose mutating tools and a read-write one does — the core safety contract
// that a diagnose/read-only run can never reach a mutating tool. readOnly must
// be built with mutate=false and readWrite with mutate=true over the same
// tool set; wantReadOnly / wantReadWrite are the expected tool counts.
func AssertRegistryDropsMutating[B tools.Boundary](t *testing.T, readOnly, readWrite *tools.Registry[B], wantReadOnly, wantReadWrite int) {
	t.Helper()
	if got := len(readOnly.Specs()); got != wantReadOnly {
		t.Errorf("read-only registry should expose %d tool(s), got %d", wantReadOnly, got)
	}
	if got := len(readWrite.Specs()); got != wantReadWrite {
		t.Errorf("read-write registry should expose %d tool(s), got %d", wantReadWrite, got)
	}
}

// AssertDenialPropagates verifies a tool surfaces a boundary denial (carrying a
// reason) instead of acting, when run against a boundary that denies the call.
func AssertDenialPropagates(t *testing.T, res tools.Result) {
	t.Helper()
	if res.Allowed {
		t.Error("tool must not act when the boundary denies the target")
	}
	if strings.TrimSpace(res.Reason) == "" {
		t.Error("a denied result should carry the boundary's reason")
	}
}
