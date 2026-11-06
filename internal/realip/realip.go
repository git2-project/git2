// Package realip restores the client's address behind the gateway.
//
// The gateway is a separate process (git2-deploy ADR 0004), so every request
// reaches git2 core from the gateway's address. Without this, the per-IP
// limits in internal/ratelimit put all clients in one bucket. The gateway
// sends the client's address in X-Real-IP (git2-deploy docs/api/gateway.md).
//
// Unlike chi's middleware.RealIP, the header is believed only on connections
// from a trusted proxy. From anywhere else it is ignored, so a client that
// reaches core directly cannot pick its own rate-limit bucket.
package realip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Header is set by the gateway to the client's address.
const Header = "X-Real-IP"

// FromTrusted replaces r.RemoteAddr with X-Real-IP when the connection comes
// from one of trusted. With no trusted prefixes it changes nothing.
func FromTrusted(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(trusted) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if peer, ok := addr(r.RemoteAddr); ok && contains(trusted, peer) {
				if client, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get(Header))); err == nil {
					r2 := r.Clone(r.Context())
					r2.RemoteAddr = netip.AddrPortFrom(client.Unmap(), 0).String()
					next.ServeHTTP(w, r2)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ParsePrefixes reads a comma-separated list of CIDRs or single addresses,
// as in TRUSTED_PROXY_CIDRS. An empty string is an empty list.
func ParsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(part)
		if err != nil {
			return nil, fmt.Errorf("%q is not a CIDR or an IP address", part)
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}

func addr(remote string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

func contains(prefixes []netip.Prefix, a netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
