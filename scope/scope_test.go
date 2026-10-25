package scope

import (
	"context"
	"testing"
)

var ctx = context.Background()

func TestStripHost(t *testing.T) {
	cases := map[string]string{
		"host":                     "host",
		"host:22":                  "host",
		"https://host/path":        "host",
		"http://host:8080/a/b":     "host",
		"ssh://user@host:22":       "host",
		"user@host":                "host",
		"1.2.3.4":                  "1.2.3.4",
		"1.2.3.4:22":               "1.2.3.4",
		"[::1]":                    "::1",     // bracketed IPv6, no port
		"[::1]:22":                 "::1",     // bracketed IPv6 with port
		"[2001:db8::1]:443":        "2001:db8::1",
		"::1":                      "::1",         // bare IPv6 literal (regression)
		"2001:db8::1":              "2001:db8::1", // bare IPv6 literal (regression)
		"  host  ":                 "host",
	}
	for in, want := range cases {
		if got := StripHost(in); got != want {
			t.Errorf("StripHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatchName(t *testing.T) {
	pats := []string{"example.com", "*.lab.internal", "10.0.0.0/8"}
	in := []string{"example.com", "www.example.com", "a.lab.internal", "EXAMPLE.COM", "example.com."}
	for _, h := range in {
		if !MatchName(h, pats) {
			t.Errorf("MatchName(%q) = false, want true", h)
		}
	}
	out := []string{"example.org", "lab.internal", "notexample.com", "10.0.0.1"}
	for _, h := range out {
		if MatchName(h, pats) {
			t.Errorf("MatchName(%q) = true, want false", h)
		}
	}
}

func TestMatchAddr(t *testing.T) {
	pats := []string{"10.0.0.0/24", "192.168.1.5"}
	if !MatchAddr("10.0.0.7", pats) {
		t.Error("10.0.0.7 should match the /24")
	}
	if !MatchAddr("192.168.1.5", pats) {
		t.Error("192.168.1.5 should match exactly")
	}
	if MatchAddr("10.0.1.1", pats) {
		t.Error("10.0.1.1 is outside the /24")
	}
	if MatchAddr("notanip", pats) {
		t.Error("a non-IP must not match")
	}
}

func TestCheckInScopeByAddr(t *testing.T) {
	if ok, reason := Check(ctx, "10.1.2.3", []string{"10.0.0.0/8"}, nil, nil); !ok {
		t.Errorf("10.1.2.3 should be in scope: %s", reason)
	}
	if ok, _ := Check(ctx, "8.8.8.8", []string{"10.0.0.0/8"}, nil, nil); ok {
		t.Error("8.8.8.8 should be denied")
	}
}

func TestCheckInScopeByName(t *testing.T) {
	// localhost resolves to 127.0.0.1 in the test environment.
	if ok, reason := Check(ctx, "localhost", []string{"127.0.0.0/8"}, nil, nil); !ok {
		t.Errorf("localhost resolves into 127.0.0.0/8 and should be in scope: %s", reason)
	}
}

func TestCheckOutOfScopeWins(t *testing.T) {
	ok, reason := Check(ctx, "10.1.2.3", []string{"10.0.0.0/8"}, []string{"10.1.2.3"}, nil)
	if ok {
		t.Fatalf("explicit out-of-scope must win over in-scope: %s", reason)
	}
	if reason == "" {
		t.Error("a denial must carry a reason")
	}
}

func TestCheckEmptyHostDenied(t *testing.T) {
	if ok, _ := Check(ctx, "", []string{"10.0.0.0/8"}, nil, nil); ok {
		t.Error("an empty host must be denied")
	}
}

func TestCheckUnresolvableFailsClosed(t *testing.T) {
	ok, reason := Check(ctx, "definitely-not-a-host.invalid", []string{"127.0.0.1"}, nil, nil)
	if ok {
		t.Fatalf("an unresolvable host must be denied: %s", reason)
	}
}

// Regression for the out-of-scope-by-IP bypass: an out-of-scope hostname must
// still be refused when it is reached by the literal IP it resolves to.
// localhost resolves to 127.0.0.1, which is also inside the in-scope range;
// without resolving the exclusion name, the IP form would slip through.
func TestCheckOutOfScopeNameReachedByIP(t *testing.T) {
	in := []string{"127.0.0.0/8"}
	out := []string{"localhost"}
	if ok, _ := Check(ctx, "localhost", in, out, nil); ok {
		t.Error("localhost is out of scope by name and must be denied")
	}
	ok, reason := Check(ctx, "127.0.0.1", in, out, nil)
	if ok {
		t.Fatalf("127.0.0.1 is the address of an out-of-scope name and must be denied: %s", reason)
	}
}
