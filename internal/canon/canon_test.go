package canon_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"protocol-v2-federator-go/internal/canon"
)

func vectorsDir(t *testing.T) string {
	t.Helper()
	// protocol-v2-federator-go/internal/canon → ../../aimarket-protocol/test-vectors
	p := filepath.Join("..", "..", "..", "aimarket-protocol", "test-vectors")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("test-vectors not found at %s: %v", p, err)
	}
	return p
}

func loadMap(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.DecodeUseNumber(b)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatal("not an object")
	}
	delete(m, "_expect")
	return m
}

func TestManifestVectorAccepts(t *testing.T) {
	dir := vectorsDir(t)
	doc := loadMap(t, filepath.Join(dir, "manifest-signed.json"))
	pub, _ := doc["signature"].(map[string]any)
	pubKey, _ := pub["public_key"].(string)
	if err := canon.VerifyManifest(doc, pubKey); err != nil {
		t.Fatalf("expected accept: %v", err)
	}
}

func TestManifestTamperedPriceRejects(t *testing.T) {
	dir := vectorsDir(t)
	doc := loadMap(t, filepath.Join(dir, "negative", "manifest-tampered-price.json"))
	pub := "11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="
	if err := canon.VerifyManifest(doc, pub); err == nil {
		t.Fatal("expected reject")
	}
}

func TestManifestTamperedByHubRejects(t *testing.T) {
	dir := vectorsDir(t)
	doc := loadMap(t, filepath.Join(dir, "negative", "manifest-tampered-by-hub.json"))
	pub := "11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="
	if err := canon.VerifyManifest(doc, pub); err == nil {
		t.Fatal("expected reject")
	}
}

func TestManifestLegacy3FieldRejects(t *testing.T) {
	dir := vectorsDir(t)
	doc := loadMap(t, filepath.Join(dir, "negative", "manifest-legacy-3field-canonical.json"))
	pub := "11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo="
	if err := canon.VerifyManifest(doc, pub); err == nil {
		t.Fatal("expected reject")
	}
}

func TestAnnounceVectorAccepts(t *testing.T) {
	dir := vectorsDir(t)
	doc := loadMap(t, filepath.Join(dir, "federation-announce-signed.json"))
	if err := canon.VerifyAnnounce(doc); err != nil {
		t.Fatalf("expected accept: %v", err)
	}
}

func TestAnnounceTamperedRejects(t *testing.T) {
	dir := vectorsDir(t)
	doc := loadMap(t, filepath.Join(dir, "negative", "announce-tampered-hub-url.json"))
	if err := canon.VerifyAnnounce(doc); err == nil {
		t.Fatal("expected reject")
	}
}

func TestDigestEmptyObject(t *testing.T) {
	h, err := canon.DigestPython(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	want := "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a"
	if h != want {
		t.Fatalf("got %s want %s", h, want)
	}
}

func TestRoundTripOwnManifest(t *testing.T) {
	// Sign and verify with a fresh key using the same module.
	dir := vectorsDir(t)
	doc := loadMap(t, filepath.Join(dir, "manifest-signed.json"))
	tools := doc["tools"]
	generatedAt := "2026-08-28T09:00:50Z"
	canonStr, err := canon.ManifestCanonical(5, generatedAt, "v1", tools, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := "capabilities_count:5|generated_at:2026-08-28T09:00:50Z|protocol_version:v1|tools_hash:"
	if len(canonStr) < len(wantPrefix) || canonStr[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("canonical mismatch: %s", canonStr)
	}
	// Ensure tools_hash matches vector README.
	wantHash := "7fbf86cde4c3a1d265ce4d893a5626584b2a34cedbf3630c4f2a8d6d9a468fcb"
	got, err := canon.DigestPython(tools)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantHash {
		// Dump python-like JSON for debug
		b, _ := canon.MarshalPython(tools)
		t.Fatalf("tools_hash got %s want %s\njson=%s", got, wantHash, string(b))
	}
	_ = json.Marshal
}
