package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// URL-validation error messages (surfaced to the admin under details.url).
const (
	urlInvalidMessage    = "URL must be a valid http(s) URL."
	urlHTTPSMessage      = "URL must use https in production."
	urlPrivateMessage    = "URL must not target a private, local or loopback address in production."
	dialTimeout          = 5 * time.Second
	dialKeepAlive        = 30 * time.Second
	tlsHandshakeTimeout  = 10 * time.Second
	expectContinueDelay  = 1 * time.Second
	idleConnTimeout      = 90 * time.Second
	maxIdleConnsPerHost  = 2
	responseHeaderTimout = 10 * time.Second
)

// blockedHostSuffixes are hostname suffixes that resolve to local/internal
// networks by convention; they are rejected by name in production (the
// dial-time guard additionally rejects by resolved IP, which is what actually
// defeats DNS rebinding).
var blockedHostSuffixes = []string{".localhost", ".local", ".internal"}

// errBlockedAddress is returned by the dial guard when a connection would
// target a private/loopback/link-local address.
var errBlockedAddress = errors.New("webhook: blocked private network address")

// ValidateWebhookURL checks a webhook URL at create/update time. In every
// environment the value must parse as an absolute http(s) URL with a host. In
// production it must additionally use https and must not name a loopback,
// private, link-local, ULA or multicast IP literal, nor a localhost/*.localhost
// /*.local/*.internal host — a static, best-effort SSRF guard complemented by
// the DNS-rebinding-resistant dial-time check in newDeliveryTransport. In
// development every http(s) URL is allowed (local receivers are the norm).
func ValidateWebhookURL(raw string, production bool) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New(urlInvalidMessage)
	}
	if !production {
		return nil
	}
	if u.Scheme != "https" {
		return errors.New(urlHTTPSMessage)
	}

	host := strings.ToLower(u.Hostname())
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return errors.New(urlPrivateMessage)
		}
		return nil
	}
	if host == "localhost" {
		return errors.New(urlPrivateMessage)
	}
	for _, suffix := range blockedHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return errors.New(urlPrivateMessage)
		}
	}
	return nil
}

// isPrivateIP reports whether ip is one PocketPress refuses to deliver to in
// production: loopback (127.0.0.0/8, ::1), RFC1918 private and RFC4193 ULA
// (via net.IP.IsPrivate), link-local unicast (169.254.0.0/16, fe80::/10),
// multicast, or the unspecified address (0.0.0.0, ::).
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

// resolverFunc resolves a host to its candidate IPs; net.DefaultResolver's
// LookupIPAddr is the production implementation, and tests inject a fake to
// exercise the dial guard offline.
type resolverFunc func(ctx context.Context, host string) ([]net.IP, error)

// defaultResolver looks host up via the system resolver.
func defaultResolver(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ips = append(ips, a.IP)
	}
	return ips, nil
}

// guardedDialContext returns a DialContext that resolves the target host,
// rejects the connection if any resolved address is private (checking the
// address it is about to connect to, not merely the URL, is what resists DNS
// rebinding), and otherwise dials the first allowed IP directly.
func guardedDialContext(resolve resolverFunc) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: dialKeepAlive}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := resolve(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("webhook: no addresses for host %q", host)
		}
		for _, ip := range ips {
			if isPrivateIP(ip) {
				return nil, fmt.Errorf("%w: %s", errBlockedAddress, ip)
			}
		}
		// Connect to the exact IP we validated so a second resolution can't
		// swap in a private address between the check and the dial.
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
}

// newDeliveryTransport builds the HTTP transport the delivery worker uses. In
// production it installs the SSRF dial guard; in development it uses a plain
// dialer so local receivers work. resolve may be nil (the system resolver).
func newDeliveryTransport(production bool, resolve resolverFunc) *http.Transport {
	t := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueDelay,
		ResponseHeaderTimeout: responseHeaderTimout,
	}
	if production {
		if resolve == nil {
			resolve = defaultResolver
		}
		t.DialContext = guardedDialContext(resolve)
	} else {
		t.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: dialKeepAlive}).DialContext
	}
	return t
}
