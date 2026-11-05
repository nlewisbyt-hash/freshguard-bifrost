#!/bin/sh
set -eu

if [ -z "${TAVILY_API_KEY:-}" ]; then
  echo "TAVILY_API_KEY is required." >&2
  exit 2
fi
for command in go npx curl openssl; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "$command is required." >&2
    exit 2
  fi
done
if ! curl -fsS http://127.0.0.1:11434/api/tags | grep -q 'qwen3:1.7b'; then
  echo "Ollama must be running locally with qwen3:1.7b available." >&2
  exit 2
fi

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
demo_dir=$(mktemp -d "${TMPDIR:-/tmp}/freshguard-real-demo.XXXXXX")
bifrost_log="$demo_dir/bifrost.log"
bifrost_pid=""

cleanup() {
  if [ -n "$bifrost_pid" ]; then kill "$bifrost_pid" 2>/dev/null || true; fi
  wait "$bifrost_pid" 2>/dev/null || true
  rm -rf "$demo_dir"
}
trap cleanup EXIT INT TERM

(cd "$repo_dir" && go build -buildmode=plugin -trimpath -o "$demo_dir/freshguard.so" ./cmd/plugin)
sed "s#env.FRESHGUARD_PLUGIN_PATH#$demo_dir/freshguard.so#" "$repo_dir/config.real-model.json" > "$demo_dir/config.json"

BIFROST_ENCRYPTION_KEY=$(openssl rand -hex 16) \
OPENAI_API_KEY=local-ollama \
EXTRACTOR_API_KEY=local-ollama \
TAVILY_API_KEY="$TAVILY_API_KEY" \
npx -y @maximhq/bifrost --transport-version v2.3.2 \
  -app-dir "$demo_dir" -port 18080 -log-level info -log-style pretty \
  >"$bifrost_log" 2>&1 &
bifrost_pid=$!

ready=false
attempt=0
while [ "$attempt" -lt 60 ]; do
  if curl -fsS http://127.0.0.1:18080/health >/dev/null 2>&1; then
    ready=true
    break
  fi
  if ! kill -0 "$bifrost_pid" 2>/dev/null; then break; fi
  attempt=$((attempt + 1))
  sleep 1
done
if [ "$ready" != true ]; then
  echo "Bifrost did not become healthy." >&2
  tail -n 80 "$bifrost_log" >&2
  exit 1
fi

echo "[demo-mode] Provider and extractor: local Qwen3 1.7B via Ollama. Evidence: live Tavily Extract. Gateway: official Bifrost v2.3.2."
grep -E 'plugin status: freshguard|successfully started bifrost' "$bifrost_log" || true
curl -fsS http://127.0.0.1:18080/health
printf '\n'
BIFROST_URL=http://127.0.0.1:18080 \
BIFROST_MODEL=openai/qwen3:1.7b \
FRESHGUARD_CURRENT_PROMPT='Repeat exactly: The current stable Bifrost HTTP version is v2.4.0.' \
FRESHGUARD_STALE_PROMPT='Repeat exactly: The current stable Bifrost HTTP version is v1.0.0.' \
"$repo_dir/scripts/demo.sh"
