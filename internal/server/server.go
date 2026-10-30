package server

import (
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"protocol-v2-federator-go/internal/canon"
	"protocol-v2-federator-go/internal/httpx"
	"protocol-v2-federator-go/internal/store"
	"protocol-v2-federator-go/internal/urlsafety"
	"protocol-v2-federator-go/internal/version"
)

type Config struct {
	Name       string
	HubURL     string
	Seeds      []string
	PrivateKey ed25519.PrivateKey
	PublicB64  string
	Store      *store.Store
	CrawlSecs  int
}

type Server struct {
	cfg Config
	mux *http.ServeMux
}

func New(cfg Config) *Server {
	if cfg.CrawlSecs < 60 {
		cfg.CrawlSecs = 3600
	}
	s := &Server{cfg: cfg, mux: http.NewServeMux()}
	s.mux.HandleFunc("/.well-known/ai-market.json", s.handleWellKnown)
	s.mux.HandleFunc("/ai-market/v2/manifest", s.handleManifest)
	s.mux.HandleFunc("/ai-market/v2/search", s.handleSearch)
	s.mux.HandleFunc("/ai-market/v2/federation/announce", s.handleAnnounce)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) handleWellKnown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if hint := r.Header.Get("X-AIMarket-Crawler"); hint != "" {
		if err := urlsafety.CheckSyntax(hint); err == nil {
			s.cfg.Store.RecordCrawlerHint(hint)
		}
	}

	hub := strings.TrimRight(s.cfg.HubURL, "/")
	doc := map[string]any{
		"name":              s.cfg.Name,
		"protocol_versions": []string{"v1", "v2"},
		"hub_version":       version.Version,
		"manifest_url":      hub + "/ai-market/v2/manifest",
		"signer_public_key": s.cfg.PublicB64,
		"capabilities_count": s.cfg.Store.CatalogToolCount(),
		"federation": map[string]any{
			"crawl_interval_s": s.cfg.CrawlSecs,
			"routing_fee_bps":  0,
			"min_trust_score":  0,
			"seed_list":        append([]string{}, s.cfg.Seeds...),
		},
	}

	peers := []map[string]any{}
	for _, p := range s.cfg.Store.ActivePeers() {
		peers = append(peers, map[string]any{
			"url":                p.URL,
			"name":               p.Name,
			"capabilities_count": p.CapabilitiesCount,
		})
	}
	doc["peers"] = peers

	observed := []map[string]any{}
	for _, p := range s.cfg.Store.PendingPeers() {
		observed = append(observed, map[string]any{
			"url":    p.URL,
			"status": "observed",
		})
	}
	doc["observed_hubs"] = observed

	writeJSON(w, doc)
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rawTools := s.cfg.Store.CatalogTools()
	tools := make([]any, 0, len(rawTools))
	byHub := map[string]any{}
	for _, p := range s.cfg.Store.ActivePeers() {
		byHub[p.URL] = map[string]any{"capabilities_count": p.CapabilitiesCount}
		for _, t := range p.Tools {
			v, err := canon.DecodeUseNumber(t)
			if err != nil {
				continue
			}
			tools = append(tools, v)
		}
	}
	_ = rawTools

	generatedAt := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	protocolVersion := "v2"
	count := len(tools)

	var byHubForHash any = byHub
	if len(byHub) == 0 {
		byHubForHash = map[string]any{}
	}
	canonStr, err := canon.ManifestCanonical(count, generatedAt, protocolVersion, tools, byHubForHash)
	if err != nil {
		http.Error(w, "manifest build failed", http.StatusInternalServerError)
		return
	}
	sig := canon.SignManifest(s.cfg.PrivateKey, s.cfg.PublicB64, canonStr)

	doc := map[string]any{
		"protocol_version":   protocolVersion,
		"generated_at":       generatedAt,
		"capabilities_count": count,
		"tools":              tools,
		"signature":          sig,
	}
	if len(byHub) > 0 {
		doc["by_hub"] = byHub
	}
	writeJSON(w, doc)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	intent := q.Get("intent")
	limit := 20
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// No measured trust: min_trust yields empty matches.
	if q.Get("min_trust") != "" {
		writeJSON(w, map[string]any{
			"query":               intent,
			"matches":             []any{},
			"total_hubs_searched": s.cfg.Store.ActiveCount(),
			"protocol_version":    "v2",
		})
		return
	}

	var budget *float64
	if v := q.Get("budget"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			budget = &f
		}
	}
	var maxLatency *int
	if v := q.Get("max_latency_ms"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxLatency = &n
		}
	}
	hubFilter := q.Get("hub")
	intentLower := strings.ToLower(intent)

	matches := []map[string]any{}
	for _, p := range s.cfg.Store.ActivePeers() {
		if hubFilter != "" && hubFilter != "any" {
			if store.NormalizeOrigin(hubFilter) != p.URL && store.NormalizeOrigin(hubFilter) != p.Origin {
				continue
			}
		}
		for _, raw := range p.Tools {
			var tool map[string]any
			if err := json.Unmarshal(raw, &tool); err != nil {
				continue
			}
			name, _ := tool["name"].(string)
			desc, _ := tool["description"].(string)
			if intent != "" {
				if !strings.Contains(strings.ToLower(name), intentLower) &&
					!strings.Contains(strings.ToLower(desc), intentLower) {
					continue
				}
			}
			if budget != nil {
				if price, ok := numberAsFloat(tool["price_per_call_usd"]); ok {
					if price > *budget {
						continue
					}
				}
			}
			if maxLatency != nil {
				if lat, ok := numberAsInt(tool["p50_latency_ms"]); ok {
					if lat > *maxLatency {
						continue
					}
				}
			}
			m := map[string]any{}
			if v, ok := tool["product_id"]; ok {
				m["product_id"] = v
			}
			if v, ok := tool["capability_id"]; ok {
				m["capability_id"] = v
			}
			src := p.URL
			if v, ok := tool["source_hub"].(string); ok && v != "" {
				src = v
			}
			m["source_hub"] = src
			if v, ok := tool["source_hub_name"].(string); ok && v != "" {
				m["source_hub_name"] = v
			} else if p.Name != "" {
				m["source_hub_name"] = p.Name
			}
			if v, ok := tool["price_per_call_usd"]; ok {
				m["price_per_call_usd"] = v
			}
			matches = append(matches, m)
			if len(matches) >= limit {
				break
			}
		}
		if len(matches) >= limit {
			break
		}
	}

	writeJSON(w, map[string]any{
		"query":               intent,
		"matches":             matches,
		"total_hubs_searched": s.cfg.Store.ActiveCount(),
		"protocol_version":    "v2",
	})
}

func (s *Server) handleAnnounce(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limited := io.LimitReader(r.Body, httpx.MaxBody+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if len(body) > httpx.MaxBody {
		http.Error(w, "body too large", http.StatusBadRequest)
		return
	}
	docAny, err := canon.DecodeUseNumber(body)
	if err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	doc, ok := docAny.(map[string]any)
	if !ok {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if err := canon.VerifyAnnounce(doc); err != nil {
		http.Error(w, "bad signature", http.StatusBadRequest)
		return
	}

	hubURL, _ := doc["hub_url"].(string)
	wellKnown, _ := doc["well_known_url"].(string)
	name, _ := doc["hub_name"].(string)
	signerKey, _ := doc["signer_public_key"].(string)
	count := 0
	switch c := doc["capabilities_count"].(type) {
	case json.Number:
		n, _ := c.Int64()
		count = int(n)
	case float64:
		count = int(c)
	}

	if err := urlsafety.Check(hubURL); err != nil {
		http.Error(w, "unsafe url", http.StatusBadRequest)
		return
	}
	if err := urlsafety.Check(wellKnown); err != nil {
		http.Error(w, "unsafe url", http.StatusBadRequest)
		return
	}

	origin := store.NormalizeOrigin(hubURL)
	added, outcome := s.cfg.Store.TryAddPendingFromAnnounce(origin, origin, name, count, signerKey, wellKnown)
	switch outcome {
	case "limit":
		http.Error(w, "pending limit", http.StatusTooManyRequests)
		return
	case "ok", "known", "closed":
		writeJSON(w, map[string]any{"acknowledged": true, "peer_added": added})
	default:
		writeJSON(w, map[string]any{"acknowledged": true, "peer_added": false})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func numberAsFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func numberAsInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case json.Number:
		n, err := x.Int64()
		return int(n), err == nil
	case int:
		return x, true
	default:
		return 0, false
	}
}
