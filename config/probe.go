package config

import _ "embed"

// probeTomlDefault is the built-in device probe rule set.
//
// probe.toml used to be a loose file next to the executable, so `probe` only
// worked when a config/ directory happened to be shipped or the working
// directory happened to contain one. Release artifacts ship only the .exe
// files, which made the feature unusable out of the box. Embedding it makes
// the built-in rules always available while still letting an on-disk
// probe.toml (or an explicit path) override them.
//
//go:embed probe.toml
var probeTomlDefault []byte

// DefaultProbeToml returns the embedded probe rule set.
func DefaultProbeToml() []byte {
	return probeTomlDefault
}
