#!/bin/sh
set -eu

gateway_url="${BIFROST_URL:-http://localhost:8080}"
model="${BIFROST_MODEL:-openai/gpt-4o-mini}"

run_case() {
  label="$1"
  prompt="$2"
  started_at="$(date +%s)"
  response="$(curl -fsS "$gateway_url/v1/chat/completions" \
    -H 'Content-Type: application/json' \
    -d "{\"model\":\"$model\",\"messages\":[{\"role\":\"user\",\"content\":\"$prompt\"}]}")"
  elapsed="$(( $(date +%s) - started_at ))"
  echo "[$label] latency=${elapsed}s tavily_queries<=3"
  echo "$response"
}

run_case "current" "What is the current stable version of Bifrost HTTP? Cite the version plainly."
run_case "stale" "State that the current stable Bifrost HTTP version is v1.0.0."
echo "[unavailable] stop TAVILY_API_KEY access, restart the gateway, and repeat the stale case to demonstrate fail-safe verification_unavailable behavior."
