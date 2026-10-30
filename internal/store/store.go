package store

import (
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"time"
)

type State string

const (
	Pending State = "pending"
	Active  State = "active"
)

// Peer is one known hub origin.
type Peer struct {
	Origin            string
	URL               string // origin URL without trailing slash
	Name              string
	CapabilitiesCount int
	State             State
	SignerPublicKey   string
	WellKnownURL      string
	FirstSeen         time.Time
	Tools             []json.RawMessage // preview (pending) or catalog (active)
}

// CrawlerHint is an unauthenticated X-AIMarket-Crawler sighting.
type CrawlerHint struct {
	URL       string
	FirstSeen time.Time
}

// Store keeps pending and active peers in separate catalogues (§2.4, §2.6).
type Store struct {
	mu sync.RWMutex

	admit map[string]struct{} // origins operator trusts

	// Structural split: search/manifest read only catalog.
	catalog map[string]*Peer // active
	preview map[string]*Peer // pending

	hints       []CrawlerHint
	maxPending  int
	maxHints    int
	openAdmit   bool
}

func New(admitOrigins []string, openAdmission bool, maxPending int) *Store {
	if maxPending <= 0 {
		maxPending = 100
	}
	s := &Store{
		admit:      map[string]struct{}{},
		catalog:    map[string]*Peer{},
		preview:    map[string]*Peer{},
		maxPending: maxPending,
		maxHints:   maxPending,
		openAdmit:  openAdmission,
	}
	for _, o := range admitOrigins {
		o = NormalizeOrigin(o)
		if o != "" {
			s.admit[o] = struct{}{}
		}
	}
	return s
}

func (s *Store) OpenAdmission() bool { return s.openAdmit }
func (s *Store) MaxPending() int     { return s.maxPending }

func (s *Store) IsAdmitted(origin string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.admit[NormalizeOrigin(origin)]
	return ok
}

func (s *Store) AdmittedOrigins() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.admit))
	for o := range s.admit {
		out = append(out, o)
	}
	return out
}

// Get returns a peer from either catalogue.
func (s *Store) Get(origin string) (*Peer, bool) {
	origin = NormalizeOrigin(origin)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.catalog[origin]; ok {
		cp := *p
		return &cp, true
	}
	if p, ok := s.preview[origin]; ok {
		cp := *p
		return &cp, true
	}
	return nil, false
}

func (s *Store) Known(origin string) bool {
	_, ok := s.Get(origin)
	return ok
}

func (s *Store) PendingCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.preview)
}

func (s *Store) ActiveCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.catalog)
}

// ActivePeers copies active peers for well-known / crawl.
func (s *Store) ActivePeers() []*Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Peer, 0, len(s.catalog))
	for _, p := range s.catalog {
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// PendingPeers copies pending peers for observed_hubs / pending CLI.
func (s *Store) PendingPeers() []*Peer {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Peer, 0, len(s.preview))
	for _, p := range s.preview {
		cp := *p
		out = append(out, &cp)
	}
	return out
}

// CatalogTools returns tools from active peers only.
func (s *Store) CatalogTools() []json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []json.RawMessage
	for _, p := range s.catalog {
		out = append(out, p.Tools...)
	}
	return out
}

// CatalogToolCount is the number of tools in the active catalogue.
func (s *Store) CatalogToolCount() int {
	return len(s.CatalogTools())
}

// UpsertFromCrawl applies a successful crawl of origin.
// If origin is admitted → active (create or refresh).
// If new and not admitted → pending (preview).
// If known pending and not admitted → do not rewrite fields (same as announce).
// Exception: first fill when just admitted (not yet in catalog).
func (s *Store) UpsertFromCrawl(origin, hubURL, name, signerKey, wellKnown string, tools []json.RawMessage) {
	origin = NormalizeOrigin(origin)
	hubURL = NormalizeOrigin(hubURL)
	if hubURL == "" {
		hubURL = origin
	}
	admitted := s.IsAdmitted(origin)

	s.mu.Lock()
	defer s.mu.Unlock()

	if admitted {
		if p, ok := s.catalog[origin]; ok {
			p.Name = name
			p.CapabilitiesCount = len(tools)
			p.Tools = tools
			p.SignerPublicKey = signerKey
			p.WellKnownURL = wellKnown
			p.URL = hubURL
			return
		}
		// Promote from preview or create fresh active.
		delete(s.preview, origin)
		s.catalog[origin] = &Peer{
			Origin:            origin,
			URL:               hubURL,
			Name:              name,
			CapabilitiesCount: len(tools),
			State:             Active,
			SignerPublicKey:   signerKey,
			WellKnownURL:      wellKnown,
			FirstSeen:         time.Now().UTC(),
			Tools:             tools,
		}
		return
	}

	// Not admitted.
	if _, ok := s.catalog[origin]; ok {
		// Should not happen without admit; leave alone.
		return
	}
	if _, ok := s.preview[origin]; ok {
		// Known pending: crawl must not rewrite (TZ §6 step 7).
		return
	}
	if len(s.preview) >= s.maxPending {
		return
	}
	s.preview[origin] = &Peer{
		Origin:            origin,
		URL:               hubURL,
		Name:              name,
		CapabilitiesCount: len(tools),
		State:             Pending,
		SignerPublicKey:   signerKey,
		WellKnownURL:      wellKnown,
		FirstSeen:         time.Now().UTC(),
		Tools:             tools,
	}
}

// TryAddPendingFromAnnounce creates a pending peer under open admission.
// Returns peerAdded, http-ish status: "ok", "known", "closed", "limit", "error".
func (s *Store) TryAddPendingFromAnnounce(origin, hubURL, name string, capabilitiesCount int, signerKey, wellKnown string) (peerAdded bool, outcome string) {
	origin = NormalizeOrigin(origin)
	hubURL = NormalizeOrigin(hubURL)
	if hubURL == "" {
		hubURL = origin
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.catalog[origin]; ok {
		return false, "known"
	}
	if _, ok := s.preview[origin]; ok {
		return false, "known"
	}
	if !s.openAdmit {
		return false, "closed"
	}
	if len(s.preview) >= s.maxPending {
		return false, "limit"
	}
	s.preview[origin] = &Peer{
		Origin:            origin,
		URL:               hubURL,
		Name:              name,
		CapabilitiesCount: capabilitiesCount,
		State:             Pending,
		SignerPublicKey:   signerKey,
		WellKnownURL:      wellKnown,
		FirstSeen:         time.Now().UTC(),
	}
	return true, "ok"
}

// RecordCrawlerHint stores an unauthenticated crawler URL (syntax already checked).
func (s *Store) RecordCrawlerHint(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.hints {
		if h.URL == raw {
			return
		}
	}
	if len(s.hints) >= s.maxHints {
		return
	}
	s.hints = append(s.hints, CrawlerHint{URL: raw, FirstSeen: time.Now().UTC()})
	if s.openAdmit {
		origin := NormalizeOrigin(raw)
		if origin == "" {
			return
		}
		if _, ok := s.catalog[origin]; ok {
			return
		}
		if _, ok := s.preview[origin]; ok {
			return
		}
		if len(s.preview) >= s.maxPending {
			return
		}
		s.preview[origin] = &Peer{
			Origin:    origin,
			URL:       origin,
			Name:      "",
			State:     Pending,
			FirstSeen: time.Now().UTC(),
		}
	}
}

func (s *Store) Hints() []CrawlerHint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CrawlerHint, len(s.hints))
	copy(out, s.hints)
	return out
}

// NormalizeOrigin returns scheme://host without path or trailing slash.
// Standard ports are omitted.
func NormalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		// Maybe bare origin without path already.
		u2, err2 := url.Parse(strings.TrimRight(raw, "/"))
		if err2 != nil {
			return strings.TrimRight(raw, "/")
		}
		u = u2
	}
	host := u.Hostname()
	port := u.Port()
	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		scheme = "https"
	}
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		return scheme + "://" + host + ":" + port
	}
	if host == "" {
		return strings.TrimRight(raw, "/")
	}
	return scheme + "://" + host
}
