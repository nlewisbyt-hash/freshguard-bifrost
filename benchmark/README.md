# Frozen Benchmark

`cases.jsonl` freezes the submission benchmark before any live run:

- 20 current claims;
- 20 deliberately stale claims;
- 10 subjective statements that should bypass search;
- 10 injected search-failure cases.

Do not remove failed cases or edit labels after seeing results. If a factual reference changes before submission, document the source change and commit the label update separately before rerunning the full set.

## Observation format

Write one JSON object per case to an observations file:

```json
{"id":"current-01","actual_status":"verified","searched":true,"blocked":false,"latency_ms":812.4,"tavily_queries":1}
```

Required fields:

- `id`: exact case identifier;
- `actual_status`: `verified`, `stale`, `unverified`, `verification_unavailable`, or `no_time_sensitive_claims`;
- `searched`: whether FreshGuard called Tavily;
- `blocked`: whether policy blocked the provider response;
- `latency_ms`: measured added latency in milliseconds;
- `tavily_queries`: number of Tavily calls, from zero to three.

The evaluator rejects missing, duplicate, unknown, or over-budget observations.

## Evaluate

Run the search-verifier benchmark with a real Tavily key:

```sh
TAVILY_API_KEY=... go run ./cmd/live-benchmark \
  -cases benchmark/cases.jsonl \
  > benchmark/observations.jsonl
```

The runner treats each objective prompt as an already-extracted atomic claim. It measures Tavily verification, not LLM claim-extraction accuracy. Subjective cases bypass search, and declared unavailable cases are injected without spending credits. Every current or stale case makes exactly one Tavily request.

To measure controlled authoritative-source mode separately, add `-source-grounded`. The runner derives one hostname from each frozen `reference_url` and sends it through Tavily `include_domains`. Write this to separate files; never overwrite the unconstrained baseline:

```sh
TAVILY_API_KEY=... go run ./cmd/live-benchmark \
  -cases benchmark/cases.jsonl -source-grounded \
  > benchmark/observations-sourced.jsonl
```

For conservative direct-page verification, use `-direct-source` and a third output file. Without a semantic judge, exact normalized containment can verify a claim; non-exact evidence remains `unverified` rather than being mislabeled as a contradiction:

```sh
TAVILY_API_KEY=... go run ./cmd/live-benchmark \
  -cases benchmark/cases.jsonl -direct-source \
  > benchmark/observations-direct.jsonl
```

If `EXTRACTOR_API_KEY` is set, direct mode automatically uses the OpenAI-compatible semantic entailment judge instead of exact containment. Override its endpoint or model with `-entailment-endpoint` and `-entailment-model`. Keep deterministic and semantic runs in separate files.

The submission semantic run uses a real local Qwen3 1.7B endpoint and live Tavily Extract:

```sh
EXTRACTOR_API_KEY=ollama TAVILY_API_KEY=... go run ./cmd/live-benchmark \
  -cases benchmark/cases.jsonl -direct-source \
  -entailment-endpoint http://127.0.0.1:11434/v1/chat/completions \
  -entailment-model qwen3:1.7b \
  > benchmark/observations-semantic.jsonl

go run ./cmd/benchmark \
  -cases benchmark/cases.jsonl \
  -observations benchmark/observations-semantic.jsonl \
  > benchmark/report-semantic.json
```

The 5 November 2026 run passed every accuracy and query gate: 85.0% stale recall, 95.2% current precision, 100.0% subjective bypass, 0.0% false blocks, 8,458.162 ms p95 latency, and 0.667 Tavily queries per case. The committed observation file contains all 60 cases.

Then calculate the frozen metrics:

```sh
go run ./cmd/benchmark \
  -cases benchmark/cases.jsonl \
  -observations benchmark/observations.jsonl \
  > benchmark/report.json
```

The report contains the confusion matrix, stale recall, current precision, subjective bypass, false block rate, p95 added latency, and average Tavily queries. Commit `observations.jsonl` and `report.json` only after a real full run; no estimated numbers belong in either file.
