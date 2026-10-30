package httpx

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"protocol-v2-federator-go/internal/urlsafety"
)

const MaxBody = 1 << 20 // 1 MiB

// SafeClient fetches remote URLs with §7.4 checks, pinned dial, no redirects.
type SafeClient struct {
	HubURL string
}

// Client is an alias used by older call sites / live tests.
type Client = SafeClient

func NewSafeClient(hubURL string) *SafeClient {
	return &SafeClient{HubURL: hubURL}
}

func New(hubURL string) *SafeClient { return NewSafeClient(hubURL) }

func (c *SafeClient) clientFor(raw string, ips []net.IP) (*http.Client, *url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	addr := net.JoinHostPort(ips[0].String(), port)
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		ResponseHeaderTimeout: 10 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		Proxy:                 nil,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client, u, nil
}

// Get fetches url after §7.4, dialing a previously checked address.
func (c *SafeClient) Get(ctx context.Context, raw string) ([]byte, int, error) {
	ips, err := urlsafety.ResolveAndCheck(raw)
	if err != nil {
		return nil, 0, err
	}
	client, u, err := c.clientFor(raw, ips)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "AIMarketHub/2.0.0")
	if c.HubURL != "" {
		req.Header.Set("X-AIMarket-Crawler", c.HubURL)
	}
	req.Host = u.Host

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, resp.StatusCode, fmt.Errorf("redirect refused: %d", resp.StatusCode)
	}
	return readBody(resp)
}

// PostJSON posts a JSON body after §7.4.
func (c *SafeClient) PostJSON(ctx context.Context, raw string, body io.Reader, contentType string) ([]byte, int, error) {
	ips, err := urlsafety.ResolveAndCheck(raw)
	if err != nil {
		return nil, 0, err
	}
	client, u, err := c.clientFor(raw, ips)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, raw, body)
	if err != nil {
		return nil, 0, err
	}
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "AIMarketHub/2.0.0")
	if c.HubURL != "" {
		req.Header.Set("X-AIMarket-Crawler", c.HubURL)
	}
	req.Host = u.Host
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	return readBody(resp)
}

func readBody(resp *http.Response) ([]byte, int, error) {
	limited := io.LimitReader(resp.Body, MaxBody+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(body) > MaxBody {
		return nil, resp.StatusCode, fmt.Errorf("body exceeds %d bytes", MaxBody)
	}
	return body, resp.StatusCode, nil
}
