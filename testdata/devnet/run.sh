#!/usr/bin/env bash
# Runs Omini against Acme's emulated network (see README.md and DESIGN.md).
# Used by `make devnet`, which builds the UI and bin/omini first.
#
#   DEVNET_PORT   UI port on this machine (default 8093)
#   DEVNET_BIND   address it listens on (default 127.0.0.1)
#   DEVNET_DATA   Omini's data folder (default data-devnet, gitignored)
#   DEVNET_SPEED  simulated seconds per real second (default 60: an hour a minute)
#   DEVNET_START  where the simulated clock starts, e.g. "tue 13:55" (default: where it stopped,
#                 or Monday 07:00 on the first run)
#   OMINI_DEVNET_PLUGINS  folder holding the omini-plugin-* repositories (default: next to this one)
set -euo pipefail
cd "$(dirname "$0")/../.."

command -v unshare >/dev/null || { echo "devnet: needs unshare (util-linux)" >&2; exit 1; }
if ! unshare --user --map-root-user --net true 2>/dev/null; then
	echo "devnet: this system does not allow unprivileged user namespaces (needed to give every emulated device its own address)" >&2
	exit 1
fi

uv sync --quiet --project testdata/devnet
exec testdata/devnet/.venv/bin/python -m emulator lab \
	--data "${DEVNET_DATA:-data-devnet}" --port "${DEVNET_PORT:-8093}" --bind "${DEVNET_BIND:-127.0.0.1}" \
	--omini bin/omini
