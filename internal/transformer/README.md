# Protocol conversion

Adapters retain their public entry points and package paths. Each adapter owns
its mutable stream state; relay attempts must continue to use fresh instances.

- `model/`: canonical request, message, response, usage and tool types. Field
  presence and opaque signature provenance have separate modules. Missing,
  explicit `null`, zero and empty values are distinct protocol inputs.
- `inbound/`: parse client requests and encode client responses. Request,
  output, stream events, output-item lifecycle and wire types live together by
  protocol. Anthropic's legacy chunk interface remains separate from its
  canonical event interface.
- `outbound/`: encode provider requests and decode provider responses. Request
  content, cache/beta configuration, signatures, passthrough and stream item
  tracking are separate from HTTP entry points. Gemini schema normalization and
  response metadata have dedicated modules.
- `protocol/`: provider wire types shared between adapters without depending on
  either adapter direction. `compat/` contains scoped compatibility helpers;
  `rawjson/` preserves raw provider fields when only a targeted edit is needed.

Keep opaque signatures unchanged and scoped to their provider/kind/tool call.
Do not replace presence-aware JSON handling with zero-value checks, consume a
stream aggregator twice, or discard native replay items during normalization.

Run `go test ./internal/transformer/...` for golden request fixtures, field
presence, signature provenance, stream ordering/finalization, and dependency
direction checks. Run `go test ./internal/relay/...` after changes to adapter
state or stream lifecycle to cover retry isolation and replay. Golden fixtures
should change only when protocol behavior is intentionally changed.
