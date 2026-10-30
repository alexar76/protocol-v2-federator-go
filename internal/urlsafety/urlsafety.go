package urlsafety

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"
)

// ErrUnsafe is returned when a URL fails §7.4.
var ErrUnsafe = fmt.Errorf("url unsafe")

// LookupIP resolves a host. Tests may replace this to avoid the network.
var LookupIP = net.LookupIP

// CheckSyntax rejects control characters, bad schemes, and userinfo without DNS.
func CheckSyntax(raw string) error {
	if raw == "" {
		return fmt.Errorf("%w: empty", ErrUnsafe)
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: control character", ErrUnsafe)
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: parse: %v", ErrUnsafe, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "http" {
		return fmt.Errorf("%w: scheme %q", ErrUnsafe, u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("%w: userinfo", ErrUnsafe)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("%w: missing host", ErrUnsafe)
	}
	return nil
}

// Check validates a URL under §7.4 (syntax + resolve + blocked ranges).
func Check(raw string) error {
	_, err := ResolveAndCheck(raw)
	return err
}

// ResolveAndCheck resolves the host and refuses private/special addresses (§7.4).
func ResolveAndCheck(raw string) ([]net.IP, error) {
	if err := CheckSyntax(raw); err != nil {
		return nil, err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: parse: %v", ErrUnsafe, err)
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if blocked(ip) {
			return nil, fmt.Errorf("%w: address %s", ErrUnsafe, ip)
		}
		return []net.IP{ip}, nil
	}
	ips, err := LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve: %v", ErrUnsafe, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%w: no addresses", ErrUnsafe)
	}
	for _, ip := range ips {
		if blocked(ip) {
			return nil, fmt.Errorf("%w: address %s", ErrUnsafe, ip)
		}
	}
	return ips, nil
}

func blocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
		if ip4[0] == 255 && ip4[1] == 255 && ip4[2] == 255 && ip4[3] == 255 {
			return true
		}
		return false
	}
	if len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc {
		return true
	}
	if len(ip) == net.IPv6len {
		zeros := true
		for i := 0; i < 12; i++ {
			if ip[i] != 0 {
				zeros = false
				break
			}
		}
		if zeros {
			return blocked(net.IPv4(ip[12], ip[13], ip[14], ip[15]))
		}
	}
	return false
}

// Origin returns scheme://host (omitting default ports), without path.
func Origin(raw string) (string, error) {
	if err := CheckSyntax(raw); err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	port := u.Port()
	scheme := strings.ToLower(u.Scheme)
	if port != "" {
		if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
			return scheme + "://" + host, nil
		}
		return scheme + "://" + net.JoinHostPort(host, port), nil
	}
	return scheme + "://" + host, nil
}

// StripTrailingSlash removes trailing slashes.
func StripTrailingSlash(s string) string {
	return strings.TrimRight(s, "/")
}
