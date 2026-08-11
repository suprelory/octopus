# Transformer quality matrix

The runtime planner refines every `conditional` cell using requested semantic
features. `native` requires raw passthrough; `lossless` uses the canonical IR
without a known loss; `unsupported` is rejected before upstream I/O.

| Inbound operation | OpenAI Chat | OpenAI Responses | Anthropic | Gemini | Volcengine Responses | OpenAI Embeddings |
| --- | --- | --- | --- | --- | --- | --- |
| OpenAI Chat / chat | conditional | conditional | conditional | conditional | conditional | unsupported |
| OpenAI Responses / responses | conditional | native | conditional | conditional | conditional | unsupported |
| Anthropic Messages / chat | conditional | conditional | native | conditional | conditional | unsupported |
| Gemini Contents / chat | conditional | conditional | conditional | conditional | conditional | unsupported |
| OpenAI Embeddings / embeddings | unsupported | unsupported | unsupported | unsupported | unsupported | lossless |
| OpenAI Images / images | unsupported | unsupported | unsupported | unsupported | unsupported | unsupported |
| Rerank / rerank | unsupported | unsupported | unsupported | unsupported | unsupported | unsupported |

Dynamic counters are available through `outbound.SnapshotCapabilityMetrics`.
They aggregate decision status and loss counts by operation, inbound format,
outbound format, and degraded field; request bodies and model output are never
retained.
