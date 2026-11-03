package webhook

import (
	"context"
	"errors"
	"net"
	"testing"
)

// TestIsPrivateIP tables the address ranges PocketPress refuses to deliver to
// in production against the public ones it allows.
func TestIsPrivateIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},      // loopback
		{"::1", true},            // loopback v6
		{"10.0.0.5", true},       // RFC1918
		{"172.16.4.9", true},     // RFC1918
		{"192.168.1.1", true},    // RFC1918
		{"169.254.10.1", true},   // link-local
		{"fe80::1", true},        // link-local v6
		{"fc00::1", true},        // ULA
		{"224.0.0.1", true},      // multicast
		{"0.0.0.0", true},        // unspecified
		{"::", true},             // unspecified v6
		{"8.8.8.8", false},       // public
		{"1.1.1.1", false},       // public
		{"93.184.216.34", false}, // public (example.com)
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("ParseIP(%q) = nil", tc.ip)
		}
		if got := isPrivateIP(ip); got != tc.want {
			t.Errorf("isPrivateIP(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
}

// TestValidateWebhookURLProduction covers the production create/update rules:
// https required, and private/localhost hosts rejected.
func TestValidateWebhookURLProduction(t *testing.T) {
	reject := []string{
		"http://example.com/hook",    // not https
		"https://127.0.0.1/hook",     // loopback literal
		"https://10.0.0.1/hook",      // private literal
		"https://172.16.0.1/hook",    // private literal
		"https://192.168.1.5/hook",   // private literal
		"https://169.254.1.1/hook",   // link-local literal
		"https://[::1]/hook",         // loopback v6 literal
		"https://[fc00::1]/hook",     // ULA v6 literal
		"https://localhost/hook",     // localhost name
		"https://api.localhost/hook", // *.localhost
		"https://box.local/hook",     // *.local
		"https://svc.internal/hook",  // *.internal
		"not-a-url",                  // unparseable
		"ftp://example.com",          // wrong scheme
	}
	for _, u := range reject {
		if err := ValidateWebhookURL(u, true); err == nil {
			t.Errorf("ValidateWebhookURL(%q, production) = nil, want rejection", u)
		}
	}

	accept := []string{
		"https://example.com/api/pocketpress-webhook",
		"https://hooks.example.com:8443/x",
		"https://93.184.216.34/hook", // public IP literal
	}
	for _, u := range accept {
		if err := ValidateWebhookURL(u, true); err != nil {
			t.Errorf("ValidateWebhookURL(%q, production) = %v, want nil", u, err)
		}
	}
}

// TestValidateWebhookURLDevelopment confirms development allows any http(s)
// URL, including localhost (local receivers), but still rejects non-http(s).
func TestValidateWebhookURLDevelopment(t *testing.T) {
	accept := []string{
		"http://localhost:3000/hook",
		"http://127.0.0.1:9000/hook",
		"https://example.com/hook",
	}
	for _, u := range accept {
		if err := ValidateWebhookURL(u, false); err != nil {
			t.Errorf("ValidateWebhookURL(%q, dev) = %v, want nil", u, err)
		}
	}
	if err := ValidateWebhookURL("javascript:alert(1)", false); err == nil {
		t.Error("ValidateWebhookURL(javascript:, dev) = nil, want rejection")
	}
}

// TestGuardedDialRejectsResolvedPrivateIP confirms the dial-time guard blocks a
// hostname that resolves to a private address (DNS-rebinding resistance) using
// an injected resolver, and blocks an IP literal directly, while allowing a
// public address to proceed to the dialer.
func TestGuardedDialRejectsResolvedPrivateIP(t *testing.T) {
	resolve := func(_ context.Context, host string) ([]net.IP, error) {
		switch host {
		case "rebind.example.com":
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		case "public.example.com":
			return []net.IP{net.ParseIP("93.184.216.34")}, nil
		default:
			return net.DefaultResolver.LookupIP(context.Background(), "ip", host)
		}
	}
	dial := guardedDialContext(resolve)

	// Hostname resolving to loopback → blocked before dialing.
	if _, err := dial(context.Background(), "tcp", "rebind.example.com:443"); !errors.Is(err, errBlockedAddress) {
		t.Fatalf("dial rebind host err = %v, want errBlockedAddress", err)
	}

	// IP literal in a private range → blocked (the resolver echoes the literal).
	literalResolve := func(_ context.Context, host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP(host)}, nil
	}
	if _, err := guardedDialContext(literalResolve)(context.Background(), "tcp", "10.0.0.1:80"); !errors.Is(err, errBlockedAddress) {
		t.Fatalf("dial private literal err = %v, want errBlockedAddress", err)
	}

	// Public host → passes the guard. Use an already-cancelled context so the
	// dial fails immediately (context canceled) without a real network attempt;
	// the point is only that it is NOT blocked as a private address.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := dial(cancelled, "tcp", "public.example.com:1"); errors.Is(err, errBlockedAddress) {
		t.Fatalf("dial public host was blocked as private, want it allowed")
	}
}
