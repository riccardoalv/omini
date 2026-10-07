#!/bin/sh
# Starts Omini. With OMINI_NMAP=install, installs nmap first (once per
# container): Omini uses it but cannot ship it (nmap's license is not MIT).
set -e
if [ "$OMINI_NMAP" = "install" ] && ! command -v nmap >/dev/null 2>&1; then
  echo "installing nmap (OMINI_NMAP=install)…"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq && apt-get install -y -qq --no-install-recommends nmap >/dev/null 2>&1 \
    && rm -rf /var/lib/apt/lists/* \
    || echo "could not install nmap: the nmap integration will say so" >&2
fi
exec /usr/local/bin/omini "$@"
