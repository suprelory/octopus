# Transformer quality matrix

The runtime planner refines every `conditional` cell using requested semantic
features. `native` requires raw passthrough; `lossless` uses the canonical IR
without a known loss; `unsupported` is rejected before upstream I/O.

| Inbound operation | OpenAI Chat | OpenAI Responses | Anthropic | Gemini | OpenAI Embeddings |
| --- | --- | --- | --- | --- | --- |
| OpenAI Chat / chat | conditional | conditional | conditional | conditional | unsupported |
| OpenAI Responses / responses | conditional | native | conditional | conditional | unsupported |
| Anthropic Messages / chat | conditional | conditional | native | conditional | unsupported |
| Gemini Contents / chat | conditional | conditional | conditional | conditional | unsupported |
| OpenAI Embeddings / embeddings | unsupported | unsupported | unsupported | unsupported | lossless |
| OpenAI Images / images | unsupported | unsupported | unsupported | unsupported | unsupported |
| Rerank / rerank | unsupported | unsupported | unsupported | unsupported | unsupported |

Dynamic counters are available through `outbound.SnapshotCapabilityMetrics`.
They aggregate decision status and loss counts by operation, inbound format,
outbound format, and degraded field; request bodies and model output are never
retained.

## Canonical IR scope

- Requests use a validated tagged operation union. Chat, Responses, and
  embeddings keep temporary legacy mirrors; images and rerank require their
  operation payload and do not synthesize incomplete legacy payloads.
- Inbound transformers record top-level field presence separately from typed
  values, preserving absent, explicit `null`, and explicit empty values.
- Provider-only options are owned by typed provider sidecars. Compatibility
  mirrors remain readable while adapters migrate to sidecar accessors.
- Canonical stream events cover chat lifecycle plus image, audio, and opaque
  events (including compact/provider events without a stable shared schema).
  Media events become semantic only when they carry data or a URI; opaque
  events become semantic only when they carry a payload.
