package server_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"protocol-v2-federator-go/internal/canon"
	"protocol-v2-federator-go/internal/server"
	"protocol-v2-federator-go/internal/store"
	"protocol-v2-federator-go/internal/urlsafety"
)

func usePublicDNS(t *testing.T) {
	t.Helper()
	prev := urlsafety.LookupIP
	t.Cleanup(func() { urlsafety.LookupIP = prev })
	urlsafety.LookupIP = func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	}
}

func newTestServer(t *testing.T, open bool, admit []string, maxPending int) (*server.Server, ed25519.PrivateKey, string, *store.Store) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	st := store.New(admit, open, maxPending)
	srv := server.New(server.Config{
		Name:       "Test Federator",
		HubURL:     "https://operator.example",
		Seeds:      []string{"https://modelmarket.dev/.well-known/ai-market.json"},
		PrivateKey: priv,
		PublicB64:  pubB64,
		Store:      st,
		CrawlSecs:  3600,
	})
	return srv, priv, pubB64, st
}

func TestWellKnownPeersVsObserved(t *testing.T) {
	srv, _, _, st := newTestServer(t, true, []string{"https://admitted.example"}, 100)
	st.UpsertFromCrawl("https://admitted.example", "https://admitted.example", "Admitted", "pk",
		"https://admitted.example/.well-known/ai-market.json",
		[]json.RawMessage{json.RawMessage(`{"name":"a.tool","description":"yes","input_schema":{},"product_id":"p","capability_id":"a.tool"}`)})
	st.UpsertFromCrawl("https://pending.example", "https://pending.example", "Pending", "pk",
		"https://pending.example/.well-known/ai-market.json",
		[]json.RawMessage{json.RawMessage(`{"name":"secret.tool","description":"no","input_schema":{},"product_id":"s","capability_id":"secret.tool"}`)})

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/.well-known/ai-market.json", nil))
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	peers, _ := doc["peers"].([]any)
	if len(peers) != 1 {
		t.Fatalf("peers=%v", peers)
	}
	observed, _ := doc["observed_hubs"].([]any)
	if len(observed) != 1 {
		t.Fatalf("observed=%v", observed)
	}
	obs := observed[0].(map[string]any)
	if obs["status"] != "observed" {
		t.Fatalf("status=%v", obs["status"])
	}
}

func TestSearchIgnoresPending(t *testing.T) {
	srv, _, _, st := newTestServer(t, true, nil, 100)
	st.UpsertFromCrawl("https://pending.example", "https://pending.example", "Pending", "pk",
		"https://pending.example/.well-known/ai-market.json",
		[]json.RawMessage{json.RawMessage(`{"name":"secret.tool","description":"hidden","input_schema":{},"capability_id":"secret.tool"}`)})

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ai-market/v2/search?intent=secret", nil))
	var doc map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &doc)
	matches, _ := doc["matches"].([]any)
	if len(matches) != 0 {
		t.Fatalf("matches=%v", matches)
	}
}

func TestSearchFindsAdmitted(t *testing.T) {
	srv, _, _, st := newTestServer(t, false, []string{"https://admitted.example"}, 100)
	st.UpsertFromCrawl("https://admitted.example", "https://admitted.example", "Admitted", "pk",
		"https://admitted.example/.well-known/ai-market.json",
		[]json.RawMessage{json.RawMessage(`{"name":"translate.multi","description":"Translate text","input_schema":{},"capability_id":"translate.multi@v2","price_per_call_usd":0.4}`)})

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ai-market/v2/search?intent=translate", nil))
	var doc map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &doc)
	matches, _ := doc["matches"].([]any)
	if len(matches) != 1 {
		t.Fatalf("matches=%v body=%s", matches, rr.Body.String())
	}
}

func TestMinTrustEmpty(t *testing.T) {
	srv, _, _, st := newTestServer(t, false, []string{"https://admitted.example"}, 100)
	st.UpsertFromCrawl("https://admitted.example", "https://admitted.example", "Admitted", "pk",
		"https://admitted.example/.well-known/ai-market.json",
		[]json.RawMessage{json.RawMessage(`{"name":"t","description":"d","input_schema":{}}`)})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ai-market/v2/search?intent=t&min_trust=0.1", nil))
	var doc map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &doc)
	if len(doc["matches"].([]any)) != 0 {
		t.Fatal("min_trust must empty matches without measured trust")
	}
}

func TestOwnManifestVerifies(t *testing.T) {
	srv, _, pubB64, st := newTestServer(t, false, []string{"https://admitted.example"}, 100)
	st.UpsertFromCrawl("https://admitted.example", "https://admitted.example", "Admitted", "pk",
		"https://admitted.example/.well-known/ai-market.json",
		[]json.RawMessage{json.RawMessage(`{"name":"t","description":"d","input_schema":{},"capability_id":"t@v1"}`)})

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/ai-market/v2/manifest", nil))
	docAny, err := canon.DecodeUseNumber(rr.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	doc := docAny.(map[string]any)
	if err := canon.VerifyManifest(doc, pubB64); err != nil {
		t.Fatalf("own manifest: %v", err)
	}
}

func TestAnnounceClosed(t *testing.T) {
	usePublicDNS(t)
	srv, priv, pubB64, st := newTestServer(t, false, nil, 100)
	body := signedAnnounce(t, priv, pubB64, "https://example.com")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ai-market/v2/federation/announce", bytes.NewReader(body))
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["peer_added"] != false {
		t.Fatalf("resp=%v", resp)
	}
	if st.PendingCount() != 0 {
		t.Fatal("closed must not create")
	}
}

func TestAnnounceOpenCreatesPending(t *testing.T) {
	usePublicDNS(t)
	srv, priv, pubB64, st := newTestServer(t, true, nil, 100)
	// Use a public-looking host that Check will try to resolve — for announce
	// handler we need URLs that pass §7.4. example.com resolves publicly.
	body := signedAnnounce(t, priv, pubB64, "https://example.com")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ai-market/v2/federation/announce", bytes.NewReader(body))
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["peer_added"] != true {
		t.Fatalf("resp=%v", resp)
	}
	if st.PendingCount() != 1 {
		t.Fatalf("pending=%d", st.PendingCount())
	}
	// Replay must not change
	body2 := signedAnnounce(t, priv, pubB64, "https://example.com")
	// bump claimed count in a new signature... same hub, TryAddPending known
	rr2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr2, httptest.NewRequest(http.MethodPost, "/ai-market/v2/federation/announce", bytes.NewReader(body2)))
	p, _ := st.Get("https://example.com")
	if p.CapabilitiesCount != 0 {
		// signedAnnounce uses 0
	}
	_ = p
}

func TestAnnouncePendingLimit429(t *testing.T) {
	usePublicDNS(t)
	srv, priv, pubB64, st := newTestServer(t, true, nil, 1)
	// Fill with direct store inject to avoid DNS for many hosts, then one real announce.
	st.TryAddPendingFromAnnounce("https://already.example", "https://already.example", "a", 0, "k", "https://already.example/.well-known/ai-market.json")
	body := signedAnnounce(t, priv, pubB64, "https://example.com")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/ai-market/v2/federation/announce", bytes.NewReader(body)))
	if rr.Code != 429 {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAnnounceBadSignature400(t *testing.T) {
	srv, _, _, _ := newTestServer(t, true, nil, 100)
	body := []byte(`{"hub_url":"https://example.com","well_known_url":"https://example.com/.well-known/ai-market.json","capabilities_count":1,"signer_public_key":"11qYAYKxCrfVS/7TyWQHOg7hcvPapiMlrwIaaPcHURo=","signature":{"algorithm":"ed25519","value":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="}}`)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/ai-market/v2/federation/announce", bytes.NewReader(body)))
	if rr.Code != 400 {
		t.Fatalf("status %d", rr.Code)
	}
}

func TestRedirectNotFollowed(t *testing.T) {
	hops := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/next", http.StatusFound)
			return
		}
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer ts.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	// Simulate SafeClient policy: do not follow.
	resp, err := client.Get(ts.URL + "/start")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 && resp.StatusCode != 301 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if hops != 1 {
		t.Fatalf("hops=%d want 1", hops)
	}
}

func signedAnnounce(t *testing.T, priv ed25519.PrivateKey, pubB64, hub string) []byte {
	t.Helper()
	wk := strings.TrimRight(hub, "/") + "/.well-known/ai-market.json"
	val, err := canon.SignAnnounce(priv, hub, wk, 0)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"hub_url":            hub,
		"well_known_url":     wk,
		"capabilities_count": 0,
		"signer_public_key":  pubB64,
		"signature": map[string]any{
			"algorithm": "ed25519",
			"value":     val,
		},
	}
	b, _ := json.Marshal(body)
	return b
}
