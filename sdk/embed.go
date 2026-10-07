// Package sdk embeds the Python SDK (omini-sdk). The core installs this copy
// into every plugin environment, so plugins always speak the protocol of the
// core that runs them.
package sdk

import "embed"

// Python is the omini-sdk source tree (python/pyproject.toml, python/src/...).
//
//go:embed python/pyproject.toml python/README.md python/src/omini_sdk/*.py
var Python embed.FS
