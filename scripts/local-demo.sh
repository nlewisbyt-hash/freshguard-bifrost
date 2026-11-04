#!/bin/sh
set -eu

if [ -z "${TAVILY_API_KEY:-}" ]; then
  echo "TAVILY_API_KEY is required; the local model fixture does not replace live Tavily evidence." >&2
  exit 2
fi
if ! command -v go >/dev/null 2>&1; then
  echo "Go 1.27.3 is required." >&2
  exit 2
fi
if ! go version | grep -q 'go1\.27\.3'; then
  echo "Go 1.27.3 is required for Bifrost plugin ABI compatibility." >&2
  exit 2
fi
if ! command -v npx >/dev/null 2>&1; then
  echo "Node.js/npx is required to launch official Bifrost v2.3.2." >&2
  exit 2
fi

repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
demo_dir=$(mktemp -d "${TMPDIR:-/tmp}/freshguard-demo.XXXXXX")
fixture_log="$demo_dir/demo-provider.log"
bifrost_log="$demo_dir/bifrost.log"
fixture_pid=""
bifrost_pid=""

cleanup() {
  if [ -n "$bifrost_pid" ]; then kill "$bifrost_pid" 2>/dev/null || true; fi
  if [ -n "$fixture_pid" ]; then kill "$fixture_pid" 2>/dev/null || true; fi
  wait "$bifrost_pid" 2>/dev/null || true
  wait "$fixture_pid" 2>/dev/null || true
  rm -rf "$demo_dir"
}
trap cleanup EXIT INT TERM

go build -buildmode=plugin -trimpath -o "$demo_dir/freshguard.so" "$repo_dir/cmd/plugin"
sed "s#env.FRESHGUARD_PLUGIN_PATH#$demo_dir/freshguard.so#" "$repo_dir/config.demo.json" > "$demo_dir/config.json"

(cd "$repo_dir" && DEMO_PROVIDER_ADDR=127.0.0.1:18081 go run ./cmd/demo-provider >"$fixture_log" 2>&1) &
fixture_pid=$!

BIFROST_ENCRYPTION_KEY=$(openssl rand -hex 16) \
OPENAI_API_KEY=deterministic-demo-fixture \
EXTRACTOR_API_KEY=deterministic-demo-fixture \
TAVILY_API_KEY="$TAVILY_API_KEY" \
npx -y @maximhq/bifrost --transport-version v2.3.2 \
  -app-dir "$demo_dir" -port 18080 -log-level info -log-style pretty \
  >"$bifrost_log" 2>&1 &
bifrost_pid=$!

ready=false
attempt=0
while [ "$attempt" -lt 45 ]; do
  if curl -fsS http://127.0.0.1:18080/health >/dev/null 2>&1; then
    ready=true
    break
  fi
  if ! kill -0 "$bifrost_pid" 2>/dev/null; then
    break
  fi
  attempt=$((attempt + 1))
  sleep 1
done

if [ "$ready" != true ]; then
  echo "Bifrost did not become healthy." >&2
  tail -n 80 "$bifrost_log" >&2
  exit 1
fi

echo "[demo-mode] Provider and extractor responses are deterministic local fixtures; Bifrost, FreshGuard, and Tavily evidence are live."
grep -E 'plugin status: freshguard|successfully started bifrost' "$bifrost_log" || true
curl -fsS http://127.0.0.1:18080/health
printf '\n'
BIFROST_URL=http://127.0.0.1:18080 BIFROST_MODEL=openai/gpt-4o-mini "$repo_dir/scripts/demo.sh"
