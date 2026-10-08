#!/usr/bin/env bash
# Runs Omini against the simulated development network (see README.md).
# Used by `make devnet`, which builds the UI and bin/omini first.
set -euo pipefail
cd "$(dirname "$0")/../.."

port=${DEVNET_PORT:-8093}
data=${DEVNET_DATA:-data-devnet}
plugins=$(ls -d "$PWD"/testdata/devnet/devnet-* | paste -sd, -)
mkdir -p "$data"

OMINI_ADDR=":$port" \
	OMINI_DATA_DIR="$data" \
	OMINI_AUTOSCAN=false \
	OMINI_PLUGIN_INDEX=off \
	OMINI_PLUGIN_DIRS="$plugins" \
	OMINI_POLL_INTERVAL=30s \
	./bin/omini &
pid=$!
trap 'kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true' EXIT INT TERM

uv run --quiet --no-project python testdata/devnet/bootstrap.py --url "http://127.0.0.1:$port" --data "$data"
wait "$pid"
