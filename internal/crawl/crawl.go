package crawl

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"protocol-v2-federator-go/internal/canon"
	"protocol-v2-federator-go/internal/httpx"
	"protocol-v2-federator-go/internal/store"
	"protocol-v2-federator-go/internal/urlsafety"
)

const MaxDepth = 3

type Crawler struct {
	Store     *store.Store
	Client    *httpx.SafeClient
	Seeds     []string
	Interval  time.Duration
	Log       *log.Logger
}

func New(st *store.Store, client *httpx.SafeClient, seeds []string, intervalSec int) *Crawler {
	if intervalSec < 60 {
		intervalSec = 3600
	}
	return &Crawler{
		Store:    st,
		Client:   client,
		Seeds:    append([]string{}, seeds...),
		Interval: time.Duration(intervalSec) * time.Second,
		Log:      log.New(os.Stderr, "crawl: ", log.LstdFlags),
	}
}

// RunOnce performs one BFS crawl from seeds.
func (c *Crawler) RunOnce(ctx context.Context) {
	type item struct {
		url   string
		depth int
	}
	seen := map[string]struct{}{}
	queue := make([]item, 0, len(c.Seeds))
	for _, s := range c.Seeds {
		queue = append(queue, item{url: s, depth: 0})
	}

	for len(queue) > 0 {
		if ctx.Err() != nil {
			return
		}
		cur := queue[0]
		queue = queue[1:]
		if cur.depth > MaxDepth {
			continue
		}
		if _, ok := seen[cur.url]; ok {
			continue
		}
		seen[cur.url] = struct{}{}

		peers, err := c.fetchOne(ctx, cur.url)
		if err != nil {
			c.Log.Printf("%s: %v", cur.url, err)
			continue
		}
		if cur.depth >= MaxDepth {
			continue
		}
		// BFS only from active peers' peer lists.
		origin := originOf(cur.url)
		if p, ok := c.Store.Get(origin); ok && p.State == store.Active {
			for _, peerURL := range peers {
				if err := urlsafety.Check(peerURL); err != nil {
					continue
				}
				queue = append(queue, item{url: peerURL, depth: cur.depth + 1})
			}
		}
	}

	// Process crawler hints that passed syntax; §7.4 before treating as fetch target.
	for _, h := range c.Store.Hints() {
		if err := urlsafety.Check(h.URL); err != nil {
			continue
		}
		wk := h.URL
		if !hasWellKnownPath(wk) {
			wk = store.NormalizeOrigin(h.URL) + "/.well-known/ai-market.json"
		}
		if _, ok := seen[wk]; ok {
			continue
		}
		if _, err := c.fetchOne(ctx, wk); err != nil {
			c.Log.Printf("hint %s: %v", wk, err)
		}
	}
}

func hasWellKnownPath(u string) bool {
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	return parsed.Path == "/.well-known/ai-market.json" || parsed.Path == "/.well-known/ai-market.json/"
}

func originOf(raw string) string {
	return store.NormalizeOrigin(raw)
}

func (c *Crawler) fetchOne(ctx context.Context, seedURL string) (peerURLs []string, err error) {
	if err := urlsafety.Check(seedURL); err != nil {
		return nil, err
	}
	body, status, err := c.Client.Get(ctx, seedURL)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("HTTP %d", status)
	}
	doc, err := canon.DecodeUseNumber(body)
	if err != nil {
		return nil, fmt.Errorf("well-known json: %w", err)
	}
	wk, ok := doc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("well-known: not an object")
	}
	for _, req := range []string{"name", "protocol_versions", "manifest_url", "signer_public_key"} {
		if _, ok := wk[req]; !ok {
			return nil, fmt.Errorf("well-known missing %s", req)
		}
	}
	name, _ := wk["name"].(string)
	manifestURL, _ := wk["manifest_url"].(string)
	signerKey, _ := wk["signer_public_key"].(string)
	if manifestURL == "" || signerKey == "" {
		return nil, fmt.Errorf("well-known: empty manifest_url or signer_public_key")
	}

	if err := urlsafety.Check(manifestURL); err != nil {
		return nil, fmt.Errorf("manifest_url: %w", err)
	}
	mbody, status, err := c.Client.Get(ctx, manifestURL)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("manifest HTTP %d", status)
	}
	mdoc, err := canon.DecodeUseNumber(mbody)
	if err != nil {
		return nil, fmt.Errorf("manifest json: %w", err)
	}
	manifest, ok := mdoc.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("manifest: not an object")
	}
	if err := canon.VerifyManifest(manifest, signerKey); err != nil {
		return nil, fmt.Errorf("manifest signature: %w", err)
	}

	toolsRaw, _ := manifest["tools"].([]any)
	tools := make([]json.RawMessage, 0, len(toolsRaw))
	for _, t := range toolsRaw {
		pb, err := canon.MarshalPython(t)
		if err != nil {
			return nil, err
		}
		tools = append(tools, json.RawMessage(pb))
	}

	origin := store.NormalizeOrigin(seedURL)
	c.Store.UpsertFromCrawl(origin, origin, name, signerKey, seedURL, tools)

	// Collect peer well-known candidates from peers array (only used if this origin is active).
	if peers, ok := wk["peers"].([]any); ok {
		for _, p := range peers {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			u, _ := pm["url"].(string)
			if u == "" {
				continue
			}
			// peers.url is an origin; crawl its well-known.
			peerURLs = append(peerURLs, store.NormalizeOrigin(u)+"/.well-known/ai-market.json")
		}
	}
	return peerURLs, nil
}

// Loop runs immediately then on Interval until ctx is done.
func (c *Crawler) Loop(ctx context.Context) {
	c.RunOnce(ctx)
	t := time.NewTicker(c.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.RunOnce(ctx)
		}
	}
}
