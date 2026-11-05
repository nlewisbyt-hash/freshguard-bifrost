# FreshGuard for Bifrost

[![CI](https://github.com/nlewisbyt-hash/freshguard-bifrost/actions/workflows/ci.yml/badge.svg)](https://github.com/nlewisbyt-hash/freshguard-bifrost/actions/workflows/ci.yml)

FreshGuard is a native [Bifrost](https://github.com/maximhq/bifrost) post-response plugin that detects time-sensitive claims, checks them with Tavily evidence, and applies an `allow`, `warn`, or `block` freshness policy before an AI response reaches the caller.

It targets the Global AI Hackathon problem statement **Open-Source Bifrost Gateway**, with an optional Tavily stale-answer-detector integration.

## Why it matters

AI gateways already control routing, cost, and reliability. FreshGuard adds a missing production control: whether a factual answer is still current. It treats missing evidence as `unverified`, never as false, and never silently upgrades a failed search into a verified result.

## What judges can see

- Atomic time-sensitive claims extracted from buffered chat-completion responses, including streams.
- At most three Tavily searches per response.
- A deterministic 0–100 score with visible components and citations.
- `allow`: preserve the provider response and attach a report to Bifrost context.
- `warn`: append a compact freshness report to the response.
- `block`: return a structured non-fallback Bifrost error only for a confirmed `stale` claim below threshold; `unverified` evidence is reported but not blocked.
- Fail-safe `verification_unavailable` behavior for timeout, rate limit, 5xx, network, or malformed Tavily responses.

## Architecture

```mermaid
flowchart LR
    U[Client] --> B[Bifrost HTTP v2.3.2]
    B --> L[LLM provider]
    L --> P[FreshGuard PostLLMHook]
    P --> X[Strict JSON claim extractor]
    X --> T[Tavily Search or Extract]
    T --> J[Strict semantic judgment]
    J --> S[Deterministic scorer / decision mapping]
    S --> A{Policy}
    A -->|allow| R[Original response + context report]
    A -->|warn| W[Response + evidence block]
    A -->|block| E[Structured Bifrost error]
```

More detail: [docs/architecture.md](docs/architecture.md).

## Requirements

- Go **1.27.3** for ABI-compatible native plugin builds.
- Bifrost HTTP **v2.3.2** / core **v1.13.1**.
- Docker with Compose for the containerized gateway path.
- `OPENAI_API_KEY`, `EXTRACTOR_API_KEY`, `TAVILY_API_KEY`, and a 32-byte `BIFROST_ENCRYPTION_KEY`.

## Quick start

```sh
cp .env.example .env
# Fill the four required values in .env.
docker compose up --build
```

In another terminal:

```sh
./scripts/demo.sh
```

The demo makes a current-version request and an intentionally stale-version request. The latter should contain a `FreshGuard freshness check` section with status, score, and source URLs.

### Credential-light local demo

If Docker and a model-provider credential are unavailable, the repository includes a clearly labeled deterministic OpenAI-compatible fixture. Bifrost v2.3.2, the native FreshGuard plugin, and Tavily extraction remain live; only the provider answer and structured extraction/judgment responses are fixed demo inputs.

```sh
TAVILY_API_KEY=... ./scripts/local-demo.sh
```

This path requires Go 1.27.3, Node.js/npx, and a real Tavily key. It must be described as a deterministic local fixture in any recording; it is not an LLM accuracy demonstration.

### Real local-model demo

For a fully live provider and extraction path without a hosted-model credential, run Ollama locally with `qwen3:1.7b`, then launch:

```sh
TAVILY_API_KEY=... ./scripts/real-model-demo.sh
```

This path uses a real local Qwen model for the provider answer, claim extraction, and semantic judgment; official Bifrost v2.3.2 hosts the native plugin and Tavily Extract supplies live authoritative evidence. `config.real-model.json` contains no secret and keeps the local endpoint bound to `127.0.0.1`.

Watch the [60-second real-model demonstration](demo/FreshGuard-Real-Model-Demo.mp4), which shows the active gateway, verified and stale outcomes, and the measured 60-case benchmark.

## Build without Docker

```sh
go build -buildmode=plugin -trimpath -o freshguard.so ./cmd/plugin
```

Native plugins must be compiled with the same Go toolchain and Bifrost core version as the gateway. The repository pins both requirements.

## Test

```sh
go test -race ./...
go vet ./...
```

The integration test builds the native `.so` and loads it in an isolated process to verify the official `Init`, `GetName`, `PreRequestHook`, `PreLLMHook`, `PostLLMHook`, and `Cleanup` exports. The same artifact has also been smoke-tested in the official Bifrost HTTP v2.3.2 binary, where it reported `freshguard - active` and `/health` returned `status: ok`.

## Configuration

`config.example.json` registers `/plugins/freshguard.so` as a `post_builtin` plugin.

| Key | Default | Purpose |
|---|---:|---|
| `enabled` | `false` | Enables verification. |
| `policy` | `warn` | `allow`, `warn`, or `block`. |
| `threshold` | `60` | Minimum acceptable score, 0–100. |
| `max_claims` | `3` | Maximum extracted claims. |
| `max_queries` | `3` | Declared Tavily query budget. The verifier hard-caps at three. |
| `timeout` | `5s` | HTTP timeout for extraction and search. |
| `trusted_domains` | `[]` | Optional authoritative domains passed to Tavily `include_domains`. |
| `trusted_urls` | `[]` | Optional authoritative HTTPS pages verified with Tavily Extract plus the configured entailment model; takes precedence over domains. |
| `extractor_endpoint` | OpenAI chat completions | JSON-capable extraction endpoint. |
| `extractor_model` | `gpt-4o-mini` | Extraction model. |

Secrets are read only from environment variables. They are not logged or included in plugin errors.

## Scoring

FreshGuard combines recency (35%), source agreement (30%), entailment (25%), and coverage (10%). All components and the final score are clamped to 0–100. Zero evidence returns `unverified`.

See [docs/evaluation.md](docs/evaluation.md) for verified behavior, current measurement status, and the pre-submission benchmark plan.

The 60-case benchmark is frozen in [`benchmark/cases.jsonl`](benchmark/cases.jsonl). After a real full run, evaluate the observation JSONL with:

```sh
go run ./cmd/benchmark -observations benchmark/observations.jsonl
```

The command refuses incomplete or duplicate runs and prints the confusion matrix plus the required submission metrics. The first complete live baseline and its limitations are documented in [`docs/evaluation.md`](docs/evaluation.md); failed gates are retained rather than hidden.

## Security and privacy

- The plugin sends only extracted claims—not the full conversation—to Tavily.
- Direct URL mode sends at most 6,000 characters of extracted authoritative evidence to the configured entailment model; page instructions are explicitly treated as untrusted data.
- API keys stay in environment variables.
- Provider errors are passed through unchanged; FreshGuard never masks them.
- Verification is bounded to three claims and three searches.
- Native Go plugins execute inside the gateway process and should be built from reviewed source.

## Limitations

- Chat Completions text streams are buffered until the final chunk, verified once, and then released or blocked. Non-streaming Responses output text and Chat audio transcripts use the same verifier; mixed text/tool responses preserve tool calls. Stream families that cannot be replayed losslessly are explicitly rejected instead of silently bypassing protection.
- Claim extraction requires an external JSON-capable model endpoint.
- Open-web baseline mode uses Tavily relevance as an entailment proxy. Direct authoritative-URL mode adds a strict semantic judgment stage that returns only `supports`, `contradicts`, or `insufficient`.
- The semantic path is covered by fixture tests and can be exercised with the real local-model demo. Measured benchmark runs remain separately labeled by verifier and model so deterministic and semantic results are never conflated.

## Disclosure

FreshGuard is an independent hackathon project. It is not an official Maxim/Bifrost or Tavily product. Bifrost and Tavily names identify integration targets only.

## License

Apache-2.0. See [LICENSE](LICENSE).
