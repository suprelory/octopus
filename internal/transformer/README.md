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
