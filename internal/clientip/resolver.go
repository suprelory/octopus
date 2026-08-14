package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
)

// Resolver extracts the originating client address only when the immediate
// peer belongs to the configured trusted proxy set. The parsed prefixes are
// immutable, so callers can swap a resolver atomically while requests run.
type Resolver struct {
	trusted []netip.Prefix
}

// ParseTrustedProxies validates and canonicalizes a comma/newline-separated
// trusted proxy list. Entries may be individual IP addresses or CIDR prefixes.
func ParseTrustedProxies(raw string) (*Resolver, string, error) {
	entries := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '，' || r == ';' || r == '；'
	})
	resolver := &Resolver{trusted: make([]netip.Prefix, 0, len(entries))}
	canonical := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, normalized, err := parseTrustedProxy(entry)
		if err != nil {
			return nil, "", err
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		resolver.trusted = append(resolver.trusted, prefix)
		canonical = append(canonical, normalized)
	}

	// Keep the stored form stable when users enter the same proxies in a
	// different order. This also makes settings exports deterministic.
	sort.Strings(canonical)
	sort.Slice(resolver.trusted, func(i, j int) bool {
		return resolver.trusted[i].String() < resolver.trusted[j].String()
	})
	return resolver, strings.Join(canonical, ","), nil
}

func parseTrustedProxy(raw string) (netip.Prefix, string, error) {
	if addr, err := netip.ParseAddr(raw); err == nil {
		addr = normalizeAddr(addr)
		return netip.PrefixFrom(addr, addr.BitLen()), addr.String(), nil
	}

	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return netip.Prefix{}, "", fmt.Errorf("trusted proxy %q must be an IP address or CIDR prefix", raw)
	}
	if prefix.Addr().Zone() != "" {
		return netip.Prefix{}, "", fmt.Errorf("trusted proxy %q must not include an IPv6 zone", raw)
	}
	if prefix.Addr().Is4In6() {
		bits := prefix.Bits() - 96
		if bits < 0 {
			return netip.Prefix{}, "", fmt.Errorf("trusted proxy %q has an invalid IPv4-mapped prefix", raw)
		}
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), bits)
	}
	prefix = prefix.Masked()
	return prefix, prefix.String(), nil
}

func normalizeAddr(addr netip.Addr) netip.Addr {
	return addr.WithZone("").Unmap()
}

// Count returns the number of configured trusted prefixes.
func (r *Resolver) Count() int {
	if r == nil {
		return 0
	}
	return len(r.trusted)
}

// TrustsAll reports whether the resolver accepts every IPv4 or IPv6 peer.
func (r *Resolver) TrustsAll() bool {
	if r == nil {
		return false
	}
	for _, prefix := range r.trusted {
		if prefix.Bits() == 0 {
			return true
		}
	}
	return false
}

// Resolve returns the client IP represented by the request. If the TCP peer
// is not trusted, forwarded headers are deliberately ignored.
func (r *Resolver) Resolve(remoteAddr string, headers http.Header) string {
	remoteIP, ok := parseRemoteAddr(remoteAddr)
	if !ok {
		return remoteAddr
	}
	if r == nil || !r.isTrusted(remoteIP) {
		return remoteIP.String()
	}

	for _, headerName := range []string{"X-Forwarded-For", "X-Real-IP"} {
		if candidate, ok := r.validateHeader(headers.Get(headerName)); ok {
			return candidate.String()
		}
	}
	return remoteIP.String()
}

func (r *Resolver) isTrusted(addr netip.Addr) bool {
	for _, prefix := range r.trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (r *Resolver) validateHeader(header string) (netip.Addr, bool) {
	items := strings.Split(header, ",")
	if len(items) == 1 && strings.TrimSpace(items[0]) == "" {
		return netip.Addr{}, false
	}
	for i := len(items) - 1; i >= 0; i-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(items[i]))
		if err != nil {
			return netip.Addr{}, false
		}
		candidate = normalizeAddr(candidate)
		if i == 0 || !r.isTrusted(candidate) {
			return candidate, true
		}
	}
	return netip.Addr{}, false
}

func parseRemoteAddr(remoteAddr string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.Trim(strings.TrimSpace(remoteAddr), "[]")
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return normalizeAddr(addr), true
}
