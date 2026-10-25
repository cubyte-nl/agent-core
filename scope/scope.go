// Package scope holds the shared access-gate primitives used by the agents
// built on agent-core. The gate is the hard boundary inside which an agent
// may operate: every tool asks it before touching a target. Out-of-scope
// targets are refused, and the gate fails closed — on any doubt (e.g. a name
// that will not resolve) the answer is no.
//
// This package carries only the agent-independent logic: parsing a target
// down to a bare host, matching names and addresses against scope patterns,
// resolving a host, and the fail-closed Check decision. Each agent keeps its
// own typed Scope (its policy, file format and extra rules) and calls into
// here.
package scope

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Authorization is the recorded authorisation basis, stamped on every report.
// The set of valid Basis values is agent-specific and lives with each agent.
type Authorization struct {
	Basis     string `yaml:"basis"`
	Operator  string `yaml:"operator"`
	Date      string `yaml:"date"`
	Reference string `yaml:"reference,omitempty"` // contract/ticket number for client work
	Client    string `yaml:"client,omitempty"`
	Confirmed bool   `yaml:"confirmed"`
}

// StripHost reduces a URL, host:port or bare host to a bare host. It handles
// IPv6: a bracketed literal ("[::1]:22", "[2001:db8::1]") yields the inner
// address, and a bare IPv6 literal ("::1", "2001:db8::1") is returned intact
// rather than being truncated at its first colon.
func StripHost(u string) string {
	u = strings.TrimSpace(u)
	for _, p := range []string{"https://", "http://", "ssh://"} {
		u = strings.TrimPrefix(u, p)
	}
	if at := strings.LastIndex(u, "@"); at >= 0 { // user@host
		u = u[at+1:]
	}
	u = strings.SplitN(u, "/", 2)[0] // strip any path
	// Bracketed IPv6, with or without a port: [::1] or [::1]:22.
	if strings.HasPrefix(u, "[") {
		if end := strings.Index(u, "]"); end >= 0 {
			return strings.TrimSpace(u[1:end])
		}
	}
	// host:port — only splits when there is exactly one colon, so a bare
	// IPv6 literal (two or more colons) is left untouched.
	if host, _, err := net.SplitHostPort(u); err == nil {
		return strings.TrimSpace(host)
	}
	return strings.TrimSpace(u)
}

// MatchName reports whether host matches any name pattern. Patterns are
// matched case-insensitively and ignoring a trailing dot; "*.example.com"
// is a wildcard and a bare "example.com" also matches its subdomains.
// Entries containing "/" are CIDRs and are skipped here.
func MatchName(host string, patterns []string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	for _, p := range patterns {
		p = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(p)), ".")
		if p == "" || strings.Contains(p, "/") {
			continue // CIDRs are not name patterns
		}
		if strings.HasPrefix(p, "*.") { // wildcard: *.example.com
			if strings.HasSuffix(host, p[1:]) {
				return true
			}
			continue
		}
		if host == p || strings.HasSuffix(host, "."+p) {
			return true
		}
	}
	return false
}

// MatchAddr reports whether addr (an IP) matches any IP or CIDR pattern.
func MatchAddr(addr string, patterns []string) bool {
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		return false
	}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if strings.Contains(p, "/") {
			if n, err := netip.ParsePrefix(p); err == nil && n.Contains(ip) {
				return true
			}
			continue
		}
		if t, err := netip.ParseAddr(p); err == nil && t == ip {
			return true
		}
	}
	return false
}

// Resolve maps a host name to its addresses; a bare IP resolves to itself. A
// nil resolver uses the system resolver.
func Resolve(ctx context.Context, host string, r *net.Resolver) ([]string, error) {
	if _, err := netip.ParseAddr(host); err == nil {
		return []string{host}, nil
	}
	if r == nil {
		r = net.DefaultResolver
	}
	raw, err := r.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, a := range raw {
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out, nil
}

// Check is the fail-closed scope decision for a single host. It returns
// (allowed, reason) and denies on any doubt. The order is deliberate:
// out-of-scope always wins over in-scope, and resolution failure is a denial.
//
// Out-of-scope entries are matched three ways: by name, by the target's
// resolved address against out-of-scope IPs/CIDRs, and — crucially — by
// resolving each out-of-scope *name* and comparing its addresses to the
// target's. The last closes the hole where an out-of-scope hostname is
// reached by the literal IP it resolves to. An out-of-scope name that does
// not itself resolve is skipped (it cannot contribute a match); only the
// target failing to resolve denies the check.
func Check(ctx context.Context, host string, in, out []string, r *net.Resolver) (bool, string) {
	host = StripHost(host)
	if host == "" {
		return false, "empty host"
	}
	addrs, err := Resolve(ctx, host, r)
	if err != nil {
		return false, fmt.Sprintf("cannot resolve %s (%v); denied", host, err)
	}
	if MatchName(host, out) {
		return false, fmt.Sprintf("%s is explicitly out of scope", host)
	}
	for _, a := range addrs {
		if MatchAddr(a, out) {
			return false, fmt.Sprintf("%s resolves to %s, which is out of scope", host, a)
		}
	}
	// Resolve out-of-scope names and deny if the target shares an address
	// with one. Without this, an out-of-scope hostname could be reached by
	// passing the IP it resolves to.
	for _, p := range out {
		p = strings.TrimSpace(p)
		if p == "" || strings.Contains(p, "/") {
			continue
		}
		if _, err := netip.ParseAddr(p); err == nil {
			continue // already covered by MatchAddr above
		}
		outAddrs, err := Resolve(ctx, p, r)
		if err != nil {
			continue // an unresolvable exclusion cannot match
		}
		for _, oa := range outAddrs {
			for _, ta := range addrs {
				if oa == ta {
					return false, fmt.Sprintf("%s resolves to %s, which %s (out of scope) also resolves to", host, ta, p)
				}
			}
		}
	}
	if MatchName(host, in) {
		return true, "in scope"
	}
	for _, a := range addrs {
		if MatchAddr(a, in) {
			return true, "in scope"
		}
	}
	return false, fmt.Sprintf("%s is not in the in-scope list", host)
}
