package store_test

import (
	"encoding/json"
	"testing"

	"protocol-v2-federator-go/internal/store"
)

func TestPendingNotInCatalog(t *testing.T) {
	st := store.New(nil, true, 100)
	st.UpsertFromCrawl("https://stranger.example", "https://stranger.example", "Stranger", "pk", "https://stranger.example/.well-known/ai-market.json",
		[]json.RawMessage{json.RawMessage(`{"name":"secret.tool","description":"x","input_schema":{}}`)})

	if st.ActiveCount() != 0 {
		t.Fatalf("active=%d", st.ActiveCount())
	}
	if st.PendingCount() != 1 {
		t.Fatalf("pending=%d", st.PendingCount())
	}
	if len(st.CatalogTools()) != 0 {
		t.Fatal("catalog must not contain pending tools")
	}
}

func TestAdmitGoesActive(t *testing.T) {
	st := store.New([]string{"https://friend.example"}, false, 100)
	st.UpsertFromCrawl("https://friend.example", "https://friend.example", "Friend", "pk", "https://friend.example/.well-known/ai-market.json",
		[]json.RawMessage{json.RawMessage(`{"name":"ok.tool","description":"x","input_schema":{}}`)})
	if st.ActiveCount() != 1 {
		t.Fatalf("active=%d", st.ActiveCount())
	}
	if st.PendingCount() != 0 {
		t.Fatalf("pending=%d", st.PendingCount())
	}
}

func TestAnnounceDoesNotRewriteKnown(t *testing.T) {
	st := store.New(nil, true, 100)
	st.TryAddPendingFromAnnounce("https://p.example", "https://p.example", "A", 3, "k", "https://p.example/.well-known/ai-market.json")
	added, outcome := st.TryAddPendingFromAnnounce("https://p.example", "https://p.example", "B", 99, "k2", "https://p.example/.well-known/ai-market.json")
	if added || outcome != "known" {
		t.Fatalf("added=%v outcome=%s", added, outcome)
	}
	p, ok := st.Get("https://p.example")
	if !ok || p.CapabilitiesCount != 3 || p.Name != "A" {
		t.Fatalf("rewritten: %+v", p)
	}
}

func TestPendingLimit(t *testing.T) {
	st := store.New(nil, true, 2)
	st.TryAddPendingFromAnnounce("https://a.example", "https://a.example", "a", 0, "k", "https://a.example/.well-known/ai-market.json")
	st.TryAddPendingFromAnnounce("https://b.example", "https://b.example", "b", 0, "k", "https://b.example/.well-known/ai-market.json")
	added, outcome := st.TryAddPendingFromAnnounce("https://c.example", "https://c.example", "c", 0, "k", "https://c.example/.well-known/ai-market.json")
	if added || outcome != "limit" {
		t.Fatalf("added=%v outcome=%s", added, outcome)
	}
}

func TestClosedAdmissionNoRecord(t *testing.T) {
	st := store.New(nil, false, 100)
	added, outcome := st.TryAddPendingFromAnnounce("https://x.example", "https://x.example", "x", 0, "k", "https://x.example/.well-known/ai-market.json")
	if added || outcome != "closed" {
		t.Fatalf("added=%v outcome=%s", added, outcome)
	}
	if st.PendingCount() != 0 {
		t.Fatal("closed admission must not create pending")
	}
}
