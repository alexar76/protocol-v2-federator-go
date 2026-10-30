# Federating directory node (Go), written from the specification

<!-- aicom-mirror-notice -->
> 🔄 Synced from the [AI-Factory monorepo](https://github.com/alexar76/aicom) (append-only history). **Pull requests are welcome** — merged PRs are imported back and re-synced here. [Issues](https://github.com/alexar76/protocol-v2-federator-go/issues) · [Pull requests](https://github.com/alexar76/protocol-v2-federator-go/pulls)

Not the Python reference hub and not a fork of it.

One binary: `federator`. An outside operator takes it to their host with their own key and joins the federation as a **directory** node (discovery, signed manifest, search over admitted peers, announce intake). Invoke, payment channels, and escrow are out of scope.

This tree was written from `aimarket-protocol/spec.md`, the schemas, and `test-vectors/`. The reference hub sources were not opened. Closed editor probes (`protocol-v2-from-spec-rs`, `protocol-v2-receipt-from-spec`) were not ported.

**While AICOM authors and maintains this process, it does not count toward the three independent implementations in [`GOVERNANCE.md`](https://github.com/alexar76/aimarket-protocol/blob/main/GOVERNANCE.md).** Say so in any handoff. It is not published to GitHub (same contour as `pingblip`).

```bash
go test ./...
go build -o federator ./cmd/federator
```

Live smoke (network; does not open channels):

```bash
go test -tags live ./internal/live/...
```

See [OPERATOR.md](OPERATOR.md).
