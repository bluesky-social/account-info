package profile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRejectNonPublicAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{name: "public IPv4", address: "93.184.216.34:443"},
		{name: "public IPv6", address: "[2606:2800:220:1:248:1893:25c8:1946]:443"},
		{name: "loopback", address: "127.0.0.1:443", wantErr: true},
		{name: "loopback IPv6", address: "[::1]:443", wantErr: true},
		{name: "RFC1918 10/8", address: "10.0.0.1:443", wantErr: true},
		{name: "RFC1918 172.16/12", address: "172.16.0.1:443", wantErr: true},
		{name: "RFC1918 192.168/16", address: "192.168.1.1:443", wantErr: true},
		{name: "link-local metadata", address: "169.254.169.254:80", wantErr: true},
		{name: "CGNAT", address: "100.64.0.1:443", wantErr: true},
		{name: "IETF protocol", address: "192.0.0.8:443", wantErr: true},
		{name: "TEST-NET-1", address: "192.0.2.1:443", wantErr: true},
		{name: "benchmarking", address: "198.18.0.1:443", wantErr: true},
		{name: "TEST-NET-2", address: "198.51.100.1:443", wantErr: true},
		{name: "TEST-NET-3", address: "203.0.113.1:443", wantErr: true},
		{name: "reserved 240/4", address: "240.0.0.1:443", wantErr: true},
		{name: "this-network 0/8", address: "0.1.2.3:443", wantErr: true},
		{name: "NAT64 metadata", address: "[64:ff9b::a9fe:a9fe]:80", wantErr: true},
		{name: "local NAT64", address: "[64:ff9b:1::1]:443", wantErr: true},
		{name: "IPv6 discard", address: "[100::1]:443", wantErr: true},
		{name: "Teredo", address: "[2001::1]:443", wantErr: true},
		{name: "IPv6 benchmarking", address: "[2001:2::1]:443", wantErr: true},
		{name: "IPv6 documentation", address: "[2001:db8::1]:443", wantErr: true},
		{name: "6to4", address: "[2002::1]:443", wantErr: true},
		{name: "site-local", address: "[fec0::1]:443", wantErr: true},
		{name: "unspecified", address: "0.0.0.0:443", wantErr: true},
		{name: "multicast", address: "224.0.0.1:443", wantErr: true},
		{name: "IPv6 ULA", address: "[fd00::1]:443", wantErr: true},
		{name: "IPv6 link-local", address: "[fe80::1]:443", wantErr: true},
		{name: "hostname not IP", address: "example.com:443", wantErr: true},
		{name: "missing port", address: "93.184.216.34", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := rejectNonPublicAddress("tcp4", test.address, nil)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidatePDSURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pds     string
		wantErr bool
	}{
		{name: "https origin", pds: "https://pds.example"},
		{name: "https with port", pds: "https://pds.example:8443"},
		{name: "http", pds: "http://pds.example", wantErr: true},
		{name: "javascript scheme", pds: "javascript:alert(1)", wantErr: true},
		{name: "file scheme", pds: "file:///etc/passwd", wantErr: true},
		{name: "no host", pds: "https://", wantErr: true},
		{name: "empty", pds: "", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validatePDSURL(test.pds)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
