package profile

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/jcalabro/atmos/xrpc"
	"github.com/jcalabro/jttp"
)

// nonPublicNets are special-purpose ranges that pass net.IP.IsGlobalUnicast
// and are not covered by IsPrivate, but are never legitimate public
// destinations (RFC 6890 special-purpose registry). The IPv6 transition
// prefixes matter because they embed an IPv4 address the network layer
// translates AFTER this guard runs — e.g. 64:ff9b::a9fe:a9fe reaches
// 169.254.169.254 through NAT64.
var nonPublicNets = []net.IPNet{
	{IP: net.IPv4(0, 0, 0, 0), Mask: net.CIDRMask(8, 32)},         // "this network" (RFC 791)
	{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)},     // CGNAT (RFC 6598)
	{IP: net.IPv4(192, 0, 0, 0), Mask: net.CIDRMask(24, 32)},      // IETF protocol assignments
	{IP: net.IPv4(192, 0, 2, 0), Mask: net.CIDRMask(24, 32)},      // TEST-NET-1
	{IP: net.IPv4(198, 18, 0, 0), Mask: net.CIDRMask(15, 32)},     // benchmarking (RFC 2544)
	{IP: net.IPv4(198, 51, 100, 0), Mask: net.CIDRMask(24, 32)},   // TEST-NET-2
	{IP: net.IPv4(203, 0, 113, 0), Mask: net.CIDRMask(24, 32)},    // TEST-NET-3
	{IP: net.IPv4(240, 0, 0, 0), Mask: net.CIDRMask(4, 32)},       // reserved (RFC 1112)
	{IP: net.ParseIP("64:ff9b::"), Mask: net.CIDRMask(96, 128)},   // NAT64 (RFC 6052)
	{IP: net.ParseIP("64:ff9b:1::"), Mask: net.CIDRMask(48, 128)}, // local NAT64 (RFC 8215)
	{IP: net.ParseIP("100::"), Mask: net.CIDRMask(64, 128)},       // discard-only (RFC 6666)
	{IP: net.ParseIP("2001::"), Mask: net.CIDRMask(32, 128)},      // Teredo (RFC 4380)
	{IP: net.ParseIP("2001:2::"), Mask: net.CIDRMask(48, 128)},    // benchmarking (RFC 5180)
	{IP: net.ParseIP("2001:db8::"), Mask: net.CIDRMask(32, 128)},  // documentation (RFC 3849)
	{IP: net.ParseIP("2002::"), Mask: net.CIDRMask(16, 128)},      // 6to4 (RFC 3056)
	{IP: net.ParseIP("fec0::"), Mask: net.CIDRMask(10, 128)},      // deprecated site-local
}

// newPublicOnlyHTTPClient returns an HTTP client for dialing
// account-controlled endpoints (PDS hosts, handle domains, DID documents).
// The dial Control hook runs after DNS resolution on every connection —
// including redirect targets — so loopback, RFC1918/ULA, link-local
// (cloud metadata), CGNAT, and multicast destinations stay unreachable
// even via DNS rebinding.
//
// Response body sizes are bounded by the atmos callers, not here: the
// xrpc client caps JSON bodies at 5MB (xrpc.maxResponseBody, enforced
// against both Content-Length and the actual stream) and the identity
// resolver caps DID/handle documents at 1MB via io.LimitReader.
func newPublicOnlyHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   rejectNonPublicAddress,
	}
	options := append(
		xrpc.ATProtoOpts(30*time.Second),
		// xrpc.Client implements its own XRPC-aware retry loop.
		jttp.WithNoRetries(),
		// WithDialContext supersedes the dial timeout/keep-alive options
		// in ATProtoOpts; this dialer carries its own.
		jttp.WithDialContext(dialer.DialContext),
		// A proxy (e.g. from HTTPS_PROXY) would dial the final destination
		// itself, so the Control hook would only ever see the proxy's
		// address — disable proxying so every destination hits the guard.
		jttp.WithNoProxy(),
	)
	return jttp.New(options...)
}

func rejectNonPublicAddress(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("split dial address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("dial address is not an IP: %q", host)
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return fmt.Errorf("refusing to dial non-public address %s", ip)
	}
	for i := range nonPublicNets {
		if nonPublicNets[i].Contains(ip) {
			return fmt.Errorf("refusing to dial non-public address %s", ip)
		}
	}
	return nil
}
