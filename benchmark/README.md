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

```sh
go run ./cmd/benchmark \
  -cases benchmark/cases.jsonl \
  -observations benchmark/observations.jsonl \
  > benchmark/report.json
```

The report contains the confusion matrix, stale recall, current precision, subjective bypass, false block rate, p95 added latency, and average Tavily queries. Commit `observations.jsonl` and `report.json` only after a real full run; no estimated numbers belong in either file.
