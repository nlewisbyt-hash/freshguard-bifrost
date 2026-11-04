# Evaluation

## Verified automated evidence

| Property | Current evidence | Status |
|---|---|---|
| Config defaults and invalid values | Unit tests | Passing |
| Empty evidence never becomes verified | Scorer unit tests | Passing |
| Claim JSON validation, dedupe, and ≤3 limit | Extractor HTTP tests | Passing |
| Tavily mapping and ≤3 query cap | `httptest` integration | Passing |
| Timeout, 429, 5xx, malformed JSON | Failure-semantics tests | Passing |
| Provider error and unsupported-response pass-through | Plugin tests | Passing |
| Allow/warn/block behavior | Plugin tests | Passing |
| Stable citation rendering | Render tests | Passing |
| Concurrent request isolation | `go test -race ./...` | Passing |
| Native `.so` build and required exports | Packaging integration test | Passing |
| Official Bifrost HTTP v2.3.2 host load | Local process smoke test; plugin status `freshguard - active`, `/health` status `ok` | Passing |

These checks prove deterministic behavior and loadable packaging. They do not by themselves prove real-world accuracy or latency.

## Submission benchmark

Before recording the final demo, evaluate a frozen set of at least 60 responses:

- 20 verified-current claims.
- 20 deliberately stale claims.
- 10 unsupported/subjective statements.
- 10 unavailable-search cases.

Label each item independently as `verified`, `stale`, `unverified`, or `verification_unavailable`. Report the confusion matrix without deleting failures.

| Metric | Formula | Submission gate |
|---|---|---:|
| Stale recall | stale correctly flagged / all stale | ≥ 0.80 |
| Current precision | current correctly verified / all verified | ≥ 0.85 |
| Subjective bypass | subjective responses not searched / all subjective | ≥ 0.90 |
| False block rate | valid responses incorrectly blocked / valid responses | ≤ 0.05 |
| Added p95 latency | plugin p95 − provider-only p95 | report measured value |
| Tavily queries | total searches / checked response | ≤ 3.0 |

No unmeasured accuracy or latency number should appear in the pitch deck.

The frozen cases now live in [`benchmark/cases.jsonl`](../benchmark/cases.jsonl). The distribution is validated by tests and cannot be evaluated with missing or duplicate observations. See [`benchmark/README.md`](../benchmark/README.md) for the observation schema.

## Failure-mode expectations

| Scenario | Expected result |
|---|---|
| No extracted claim | `no_time_sensitive_claims`; response unchanged |
| No Tavily results | `unverified` |
| Tavily timeout/network/429/5xx/malformed payload | `verification_unavailable` |
| Existing provider error | Original error and response unchanged; no Tavily call |
| Warn policy below threshold | Original answer plus evidence block |
| Block policy below threshold | HTTP-oriented Bifrost error, fallbacks disabled |

## Reproducible commands

```sh
go test -race ./...
go vet ./...
go build -buildmode=plugin -trimpath -o freshguard.so ./cmd/plugin
go run ./cmd/benchmark -cases benchmark/cases.jsonl -observations benchmark/observations.jsonl
```
