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

### Baseline live run — 4 November 2026

The complete source-unconstrained search-verifier run is committed as [`observations.jsonl`](../benchmark/observations.jsonl) and [`report.json`](../benchmark/report.json). Each objective prompt was supplied as an already-extracted atomic claim, so this run measures Tavily verification rather than LLM extraction.

| Metric | Measured result | Gate | Outcome |
|---|---:|---:|---|
| Stale recall | 90.0% | ≥80% | Pass |
| Current precision | 66.7% | ≥85% | Fail |
| Subjective bypass | 100.0% | ≥90% | Pass |
| False block rate | 53.3% | ≤5% | Fail |
| Added p95 latency | 510.097 ms | Report | Measured |
| Average Tavily queries | 0.667 per case | ≤3.0 | Pass |

This baseline is intentionally not presented as production-ready accuracy. Sixteen of twenty current claims concerned an event page that unconstrained Tavily search did not reliably retrieve; Tavily sometimes answered `No` because it found similarly named events but no Geek Room source. The result establishes the next technical priority: authoritative-domain/source discovery before scoring. The two failed gates remain visible rather than being removed or relabeled after the run.

### Authoritative-domain comparison — 4 November 2026

The separate [`observations-sourced.jsonl`](../benchmark/observations-sourced.jsonl) and [`report-sourced.json`](../benchmark/report-sourced.json) run constrained each objective case to the hostname in its frozen `reference_url`. It did not overwrite the open-web baseline.

| Metric | Open web | Source-grounded | Change |
|---|---:|---:|---:|
| Stale recall | 90.0% | 100.0% | +10.0 pp |
| Current precision | 66.7% | 100.0% | +33.3 pp |
| Subjective bypass | 100.0% | 100.0% | 0 pp |
| False block rate | 53.3% | 53.3% | 0 pp |
| p95 added latency | 510.097 ms | 5,322.031 ms | +4,811.934 ms |

Domain restriction eliminated false verification among the stale cases but did not recover sixteen current claims whose detailed FAQ facts were absent from Tavily's retrievable page content. This proves domain selection alone is insufficient: the next iteration needs direct page extraction or pre-indexed authoritative documents, plus a latency budget. The source-grounded results are a controlled mode, not an open-web accuracy claim.

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
TAVILY_API_KEY=... go run ./cmd/live-benchmark -cases benchmark/cases.jsonl > benchmark/observations.jsonl
TAVILY_API_KEY=... go run ./cmd/live-benchmark -cases benchmark/cases.jsonl -source-grounded > benchmark/observations-sourced.jsonl
go run ./cmd/benchmark -cases benchmark/cases.jsonl -observations benchmark/observations.jsonl
```
