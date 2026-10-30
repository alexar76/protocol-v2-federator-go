//go:build live

package live_test

import (
	"context"
	"testing"

	"protocol-v2-federator-go/internal/canon"
	"protocol-v2-federator-go/internal/httpx"
)

// Live smoke: read modelmarket.dev well-known and verify its manifest.
// Does not call channel/open or channel/close.
func TestLiveModelmarketManifest(t *testing.T) {
	ctx := context.Background()
	c := httpx.NewSafeClient("https://federator-live-test.example")
	body, status, err := c.Get(ctx, "https://modelmarket.dev/.well-known/ai-market.json")
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 {
		t.Fatalf("status %d", status)
	}
	docAny, err := canon.DecodeUseNumber(body)
	if err != nil {
		t.Fatal(err)
	}
	wk := docAny.(map[string]any)
	manifestURL, _ := wk["manifest_url"].(string)
	pub, _ := wk["signer_public_key"].(string)
	if manifestURL == "" || pub == "" {
		t.Fatal("missing manifest_url or signer_public_key")
	}
	mbody, status, err := c.Get(ctx, manifestURL)
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 {
		t.Fatalf("manifest status %d", status)
	}
	mdocAny, err := canon.DecodeUseNumber(mbody)
	if err != nil {
		t.Fatal(err)
	}
	if err := canon.VerifyManifest(mdocAny.(map[string]any), pub); err != nil {
		t.Fatalf("manifest verify: %v", err)
	}
}
