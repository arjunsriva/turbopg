#!/usr/bin/env bash
# Run the allowlisted official turbopuffer-python custom tests against a
# running turbopg-server. Requires TURBOPUFFER_BASE_URL and TURBOPUFFER_API_KEY.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
ALLOWLIST="$ROOT/tests/official-python/allowlist.txt"
SDK_REF="${TURBOPUFFER_PYTHON_REF:-07bf1295bf7ca5324a33b2509084b8a2219752a2}"

if [[ -z "${TURBOPUFFER_BASE_URL:-}" ]]; then
  echo "TURBOPUFFER_BASE_URL is required (e.g. http://127.0.0.1:8080)" >&2
  exit 2
fi
if [[ -z "${TURBOPUFFER_API_KEY:-}" ]]; then
  echo "TURBOPUFFER_API_KEY is required" >&2
  exit 2
fi
unset TURBOPUFFER_REGION || true

SDK_DIR="${TURBOPUFFER_PYTHON_DIR:-}"
if [[ -z "$SDK_DIR" ]]; then
  local_clone="$ROOT/_reference/turbopuffer/repos/turbopuffer-python"
  if [[ -d "$local_clone/tests/custom" ]]; then
    SDK_DIR="$local_clone"
  else
    SDK_DIR="$(mktemp -d /tmp/turbopuffer-python.XXXXXX)"
    git clone --depth 1 https://github.com/turbopuffer/turbopuffer-python.git "$SDK_DIR"
    git -C "$SDK_DIR" fetch --depth 1 origin "$SDK_REF"
    git -C "$SDK_DIR" checkout "$SDK_REF"
  fi
fi

PYTHON="${PYTHON:-python3}"
if ! "$PYTHON" -c "import turbopuffer, pytest, numpy" 2>/dev/null; then
  "$PYTHON" -m pip install -q -e "$SDK_DIR" pytest pytest-asyncio numpy
fi

cd "$SDK_DIR"
# shellcheck disable=SC2046
exec "$PYTHON" -m pytest \
  -o addopts= \
  --asyncio-mode=auto \
  -p no:cacheprovider \
  --tb=short \
  $(grep -v '^#' "$ALLOWLIST" | grep -v '^[[:space:]]*$')
