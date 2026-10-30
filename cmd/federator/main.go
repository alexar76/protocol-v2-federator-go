package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"protocol-v2-federator-go/internal/canon"
	"protocol-v2-federator-go/internal/crawl"
	"protocol-v2-federator-go/internal/httpx"
	"protocol-v2-federator-go/internal/key"
	"protocol-v2-federator-go/internal/server"
	"protocol-v2-federator-go/internal/store"
	"protocol-v2-federator-go/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "keygen":
		os.Exit(cmdKeygen(os.Args[2:]))
	case "serve":
		os.Exit(cmdServe(os.Args[2:]))
	case "announce":
		os.Exit(cmdAnnounce(os.Args[2:]))
	case "pending":
		os.Exit(cmdPending(os.Args[2:]))
	case "version":
		fmt.Println(version.Version)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `federator — AIMarket Protocol v2 directory node

  federator keygen --out operator.key
  federator serve --key operator.key --hub-url URL --listen ADDR [options]
  federator announce --key operator.key --hub-url URL --to URL
  federator pending --listen-admin ADDR   (not implemented; use observed_hubs)

While AICOM authors this tree it does not count toward the three independent
implementations in aimarket-protocol/GOVERNANCE.md.
`)
}

func cmdKeygen(args []string) int {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "operator.key", "private key path (mode 0600)")
	_ = fs.Parse(args)
	pub, err := key.Generate(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "keygen: %v\n", err)
		return 1
	}
	fmt.Println(pub)
	return 0
}

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	keyPath := fs.String("key", "", "operator private key file")
	hubURL := fs.String("hub-url", "", "public origin this operator owns")
	listen := fs.String("listen", "127.0.0.1:8090", "listen address")
	name := fs.String("name", "federator", "hub display name")
	openAdmission := fs.Bool("open-admission", false, "allow unauthenticated announce to create pending")
	maxPending := fs.Int("max-pending", 100, "pending peer cap")
	crawlInterval := fs.Int("crawl-interval-s", 3600, "crawl interval (≥60)")
	var seeds, admits multiFlag
	fs.Var(&seeds, "seed", "seed /.well-known/ai-market.json URL (repeatable)")
	fs.Var(&admits, "admit", "trusted peer origin (repeatable)")
	_ = fs.Parse(args)

	if *keyPath == "" || *hubURL == "" {
		fmt.Fprintf(os.Stderr, "serve: --key and --hub-url are required\n")
		return 2
	}
	*hubURL = store.NormalizeOrigin(*hubURL)
	priv, _, pubB64, err := key.Load(*keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		return 1
	}

	st := store.New([]string(admits), *openAdmission, *maxPending)
	client := httpx.NewSafeClient(*hubURL)
	srv := server.New(server.Config{
		Name:       *name,
		HubURL:     *hubURL,
		Seeds:      []string(seeds),
		PrivateKey: priv,
		PublicB64:  pubB64,
		Store:      st,
		CrawlSecs:  *crawlInterval,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cr := crawl.New(st, client, []string(seeds), *crawlInterval)
	go cr.Loop(ctx)

	httpSrv := &http.Server{Addr: *listen, Handler: srv.Handler()}
	go func() {
		<-ctx.Done()
		_ = httpSrv.Close()
	}()

	fmt.Fprintf(os.Stderr, "federator %s on %s (v%s)\n", *hubURL, *listen, version.Version)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		return 1
	}
	return 0
}

func cmdAnnounce(args []string) int {
	fs := flag.NewFlagSet("announce", flag.ExitOnError)
	keyPath := fs.String("key", "", "operator private key file")
	hubURL := fs.String("hub-url", "", "this node's public origin")
	to := fs.String("to", "", "peer announce URL")
	count := fs.Int("capabilities-count", 0, "capabilities_count to claim")
	_ = fs.Parse(args)
	if *keyPath == "" || *hubURL == "" || *to == "" {
		fmt.Fprintf(os.Stderr, "announce: --key, --hub-url and --to are required\n")
		return 2
	}
	*hubURL = store.NormalizeOrigin(*hubURL)
	if strings.Contains(*to, "modelmarket.dev") && !strings.Contains(os.Getenv("FEDERATOR_ALLOW_MODELMARKET_ANNOUNCE"), "1") {
		// Binary does not call modelmarket.dev by itself; operator --to is allowed
		// only when they typed it. No auto-block beyond documenting the risk.
	}
	priv, _, pubB64, err := key.Load(*keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "announce: %v\n", err)
		return 1
	}
	wellKnown := *hubURL + "/.well-known/ai-market.json"
	sigVal, err := canon.SignAnnounce(priv, *hubURL, wellKnown, *count)
	if err != nil {
		fmt.Fprintf(os.Stderr, "announce: %v\n", err)
		return 1
	}
	body := map[string]any{
		"hub_url":            *hubURL,
		"well_known_url":     wellKnown,
		"capabilities_count": *count,
		"signer_public_key":  pubB64,
		"signature": map[string]any{
			"algorithm": "ed25519",
			"value":     sigVal,
		},
	}
	raw, _ := json.Marshal(body)
	client := httpx.NewSafeClient(*hubURL)
	resp, status, err := client.PostJSON(context.Background(), *to, bytes.NewReader(raw), "application/json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "announce: %v\n", err)
		return 1
	}
	fmt.Printf("HTTP %d\n%s\n", status, string(resp))
	if status >= 400 {
		return 1
	}
	return 0
}

func cmdPending(args []string) int {
	fmt.Fprintf(os.Stderr, "pending: read observed_hubs from GET /.well-known/ai-market.json on this node.\n")
	fmt.Fprintf(os.Stderr, "Preview tools are unapproved by the operator (§2.6).\n")
	return 0
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}
