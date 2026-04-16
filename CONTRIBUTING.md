# Contributing to EvolvX

Thank you for considering a contribution. EvolvX follows a conservative
architecture — changes are incremental, tested, and backwards-compatible with
NOFX by default.

## Ground rules

1. **Do not modify any existing NOFX package.** `trader/`, `decision/`, `market/`, `mcp/`, `store/`, `backtest/`, `web/` — these are inherited from NOFX and changes belong in the upstream repository.

2. **Every new feature must have a test.** The six invariants described in the README are non-negotiable. Any feature that violates them will be rejected.

3. **The pipeline is the only execution path.** No PR that adds a direct `PlaceOrder()` call outside of `engine/adapters/` will be merged.

4. **Strategy records are immutable.** No PR that adds an `Update()` method to the registry will be merged.

## Development setup

```bash
git clone https://github.com/JPeetz/EvolvX.git
cd EvolvX
go mod download
go test ./...   # all tests must pass on a clean checkout
```

## Good first issues

- UI panels for registry and journal (React, connects to existing API handlers)
- Outcome auto-recording from NOFX fill events
- Strategy diff viewer (compare two versions side by side)
- Prometheus metrics exporter for MetricsCollector

## PR checklist

- [ ] `go test ./...` passes
- [ ] `go vet ./...` clean
- [ ] No changes to existing NOFX packages
- [ ] New functionality has at least one test
- [ ] PR description explains *why* not just *what*

## Questions?

Open a GitHub Discussion for architecture questions. Open an Issue for bugs.
For direct contact: [@joerg_peetz](https://x.com/joerg_peetz) on X.
