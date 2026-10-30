package urlsafety_test

import (
	"net"
	"strings"
	"testing"

	"protocol-v2-federator-go/internal/urlsafety"
)

func TestBlockedAddresses(t *testing.T) {
	addrs := []string{
		"http://127.0.0.1/",
		"http://10.0.0.1/",
		"http://192.168.0.1/",
		"http://172.16.0.1/",
		"http://169.254.1.1/",
		"http://100.64.0.1/",
		"http://[::1]/",
		"http://[fc00::1]/",
		"http://[fe80::1]/",
		"http://[::ffff:127.0.0.1]/",
	}
	for _, u := range addrs {
		if err := urlsafety.Check(u); err == nil {
			t.Errorf("expected reject for %s", u)
		}
	}
}

func TestControlCharacter(t *testing.T) {
	if err := urlsafety.CheckSyntax("https://example.com/\x01evil"); err == nil {
		t.Fatal("expected reject for control character")
	}
}

func TestUserinfoRejected(t *testing.T) {
	if err := urlsafety.CheckSyntax("https://user:pass@example.com/"); err == nil {
		t.Fatal("expected reject for userinfo")
	}
}

func TestPublicHostAllowedWithInjectedDNS(t *testing.T) {
	prev := urlsafety.LookupIP
	t.Cleanup(func() { urlsafety.LookupIP = prev })
	urlsafety.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	}
	if err := urlsafety.Check("https://peer.example/.well-known/ai-market.json"); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateDNSRejected(t *testing.T) {
	prev := urlsafety.LookupIP
	t.Cleanup(func() { urlsafety.LookupIP = prev })
	urlsafety.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("10.0.0.5")}, nil
	}
	err := urlsafety.Check("https://evil.example/")
	if err == nil || !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("err=%v", err)
	}
}
