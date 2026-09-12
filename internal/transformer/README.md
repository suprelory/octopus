# Protocol conversion contracts

Adapters retain their public entry points and package paths. Relay attempts use
fresh adapter instances for provider parsing and wire encoding state.

- `model/`: canonical request, message, response, usage and tool types. Missing,
  explicit `null`, zero and empty values remain distinct protocol inputs.
- `inbound/`: client request parsing and response encoding, including wire item
  and content lifecycles.
- `outbound/`: provider request building, response parsing, cache/beta settings,
  signatures, passthrough and stream item tracking.
- `protocol/`: shared provider wire types. `compat/` contains scoped conversion
  helpers; `rawjson/` preserves raw fields during targeted edits.

Keep opaque signatures unchanged and scoped to their provider, kind and tool
call. Preserve field presence and native replay items. Avoid consuming an
aggregator twice. Run transformer tests for request fixtures, signatures and
stream ordering, and relay tests for retry isolation and replay. Golden fixtures
change only when protocol behavior changes intentionally.

`InternalLLMRequest.Operation` is the authoritative endpoint payload. Outbound
builders and replay use `ChatPayload`, `ResponsesPayload`, `EmbeddingsPayload`,
`ImagesPayload`, `RerankPayload`, and `ConversationMessages`. Shared routing and
generation options remain on the request.

Legacy-only callers remain supported. `NormalizeOperation` lifts their payload
into an operation and populates deprecated fields for compatibility. Callers that
provide both representations must keep them consistent; builders and capability
planning reject conflicts before upstream submission. Use
`SetConversationMessages`, `SetOpenAIRawInputItems`, and
`SetOpenAIResponsesOptions` when modifying a cloned request for conversion or
replay. Empty authoritative fields never recover stale provider sidecars.

The deprecated byte and aggregate streaming methods remain compatibility APIs
for at least one release cycle. Relay uses canonical events and one per-stream
`Push`/`Finish` converter. Provider adapters parse provider state, the canonical
finalizer owns message and block completion, and inbound encoders serialize the
result. A terminal marker can repair missing canonical stops; a source error
cannot. `Finish` seals the stream for every termination cause.

`ProtocolDescriptor.FieldRules` records each source semantic, target wire field,
action, condition, and reason. `outbound.BuildRequest` returns the HTTP request
and its conversion report without consuming the body or sending any bytes.
Capability planning runs this same local build with the effective upstream model.
Scalar rules inspect emitted JSON; tools are checked against emitted definitions
and schemas; provider preparation helpers report structured repairs and losses.
HTTP and transformed WebSocket submission persist and enforce the returned report.
Strict capability policy rejects known semantic losses before submission.
Dropping only unknown top-level fields is an allowed fallback under every policy.
Passthrough is planned separately because its preserved raw fields do not use
canonical builders.

Each attempt records `conversion.mode` (`lossless_canonical`, `raw_sidecar`, or
`lossy_canonical`), `replay_available`, `raw_input_preserved`, and `exact_replay`.
Replay availability describes intact input for the selected target; it does not
promise that a remote conversation ID remains live. Lossy conversions retain the
raw-preservation flag but cannot claim intact replay. `Operation.Recovery` stores
unknown top-level native fields separately from their required-field list.
Routing prefers channels that preserve these fields, then falls back to other
protocols by omitting them and recording each dropped field in the conversion
report. The original request and recovery sidecar remain intact for retries.
All builders still validate required sidecars and native input/tool semantics
before encoding. Missing required sidecars or native semantics without a target
recovery path remain hard rejections, independent of the degradation policy.

The shared contract matrix loads `testdata/contracts/<descriptor name>.json` for
every registered adapter. Registration requires request/response wire fixtures,
a normal stream with usage and a terminal event, malformed JSON, an abrupt-stream
prefix, and declarations for text, thinking, signatures, tools, citations, and
audio. Unsupported semantics require an explicit reason. The matrix exercises
every supported inbound/outbound pair and rejects unsupported operation pairs.
Source contracts also exercise split frames, CRLF, multiline data, empty terminal
data, concurrent readers, concurrent close, and data followed by a source error.

Canonical events retain immutable `StreamProvenance`: API format, wire event
type/ID, source sequence/transport, provider event type/sequence, and raw bytes.
The wire event type remains separate from an inferred JSON type. Every event has
semantic importance; finalizer repairs also carry their triggering provenance
and are marked synthesized. `StreamReplay.Push` reconstructs original source
frames once, excludes synthesized boundaries, and rejects missing provenance,
cross-protocol replay, and conflicting or regressing source sequences.

Response and output-item boundaries, MCP calls, computer actions, server tools,
grounding, annotations, and native audio remain explicit events. Their raw fields
are not flattened into client function calls or text. Unknown events are opaque.
The aggregate response is a compatibility/metrics projection; native replay uses
the canonical events. Ordinary wire encoders return `StreamConversionLoss` when
a native semantic has no target representation, and attempts record the loss in
stream diagnostics. Native passthrough observes the canonical lifecycle without
encoding and discarding a lossy projection. OpenAI URL/file/container citations
keep native annotations; Gemini grounding retains unknown retrieval metadata.

SSE source and passthrough observation share one bounded framing decoder. A
stateless `SourceEventInspector` can preview complete data lines to release
semantic precommit while retaining trailing event types, IDs and multiline data
for final conversion. `SourceEvent.Decoded` is an immutable, optional provider
DTO cache; changing `Data` requires clearing it. The cache keeps payload fields
separate from mutable SSE envelope metadata. Native WebSocket observation and
canonical conversion reuse the same DTO, including raw output, usage and retry
information. See [decoder benchmarks](../relay/stream/DECODER_BENCHMARKS.md) for
reproduction commands, before/after measurements and their scope.

Wire references: [Responses streaming events](https://developers.openai.com/api/reference/resources/responses/streaming-events),
[Anthropic messages](https://platform.claude.com/docs/en/api/beta/messages), and
[Gemini generate content](https://ai.google.dev/api/generate-content).
