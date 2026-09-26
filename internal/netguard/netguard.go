package netguard

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

var ErrBlockedAddress = errors.New("address not allowed")

// Ranges IsGlobalUnicast and IsPrivate do not already rule out.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, includes broadcast
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64: embeds an IPv4 address
	netip.MustParsePrefix("100::/64"),        // discard-only
	netip.MustParsePrefix("2001::/32"),       // Teredo: embeds an IPv4 address
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2002::/16"),       // 6to4: embeds an IPv4 address
}

func IsPublic(addr netip.Addr) bool {
	addr = addr.Unmap() // ::ffff:127.0.0.1 is really 127.0.0.1

	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// Control is a net.Dialer.Control func: called with the address already
// resolved, just before connecting. This is what makes the check immune to
// URL tricks, DNS rebinding, and redirects.
func Control(network, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !IsPublic(ap.Addr()) {
		return ErrBlockedAddress // fail closed, same error either way
	}
	return nil
}

func NewTransport() *http.Transport {
	return NewTransportWithControl(Control)
}

// NewTransportWithControl lets tests swap in a different dial check.
func NewTransportWithControl(control func(network, address string, c syscall.RawConn) error) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   control,
	}
	return &http.Transport{
		Proxy:                 nil, // a proxy would hide the real target address from Control
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
