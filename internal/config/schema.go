package config

import (
	"embed"
)

// v3SchemaFS embeds the v3 project config JSON Schema so ConfigService.
// GetSchema always serves the real, complete schema — not a relative-path
// read of docs/schema/agent-runtime.v3.json that only works when the
// daemon happens to be started from inside the repo checkout (B8: every
// installed binary got a 2-property stub schema instead).
//
//go:embed schema/agent-runtime.v3.json
var v3SchemaFS embed.FS

// V3JSONSchemaRaw returns the raw JSON bytes of the v3 config schema.
func V3JSONSchemaRaw() []byte {
	data, err := v3SchemaFS.ReadFile("schema/agent-runtime.v3.json")
	if err != nil {
		// Unreachable in a correctly built binary: the file is embedded at
		// compile time, so a missing file is a build-time, not run-time,
		// failure.
		panic("config: embedded schema missing: " + err.Error())
	}
	return data
}
