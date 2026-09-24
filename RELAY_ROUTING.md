# Relay routing

Text HTTP, Responses WebSocket, Images and Responses Compact use one budget per
logical request. Channel and submission limits count actual upstream sends, not
disabled channels or capability rejections. Backoff consumes the same time budget.
Budget exhaustion returns `504 / relay_timeout` and does not trip upstream circuits.

The global settings are `relay_max_channel_attempts`, `relay_max_total_attempts`
and `relay_failover_timeout_seconds`. Images and Compact can override each limit
with `relay_{images,compact}_{max_channel_attempts,max_total_attempts,timeout_seconds}`;
zero inherits the global value. The reliability settings page exposes these values.

Images make one generation attempt per candidate (no automatic repeat of a
possibly expensive generation on the same key). Compact honors the group's retry
limit. Image connections and first content share the precommit deadline, including
the response-header wait. SSE comments and metadata do not commit a response.
Once image content starts, the precommit timer stops and failures cannot change
the route. Client cancellation still stops the active upstream request.

Credential authentication and permission errors isolate the key across models.
Explicit key quota/rate-limit errors (`key_quota`, `key_rate_limit`, `api key quota`,
or `per-key`) do the same. Ambiguous quota/rate-limit errors are conservatively
treated as shared channel failures. Unsupported models isolate the channel/model;
network and service failures affect the channel. A key failure does not contribute
to the channel health penalty. Failure scope is included in each attempt log.

Before output, stateless requests can try another healthy key in the same channel
after a key-scoped rejection. They never repeat that failed key in the request.
All replacement sends use the original budget. Native `previous_response_id`
continuations keep their own recovery rules and cannot switch keys this way.

Ordinary affinity has `off`, `prefer` (default), and `strict` modes. Prefer can
migrate after a failure or channel cooldown. Strict restricts an existing live
binding to its channel; a missing/disabled/broken channel fails rather than
migrating. The first successful request establishes the binding. Responses replay
preferences and native continuation recovery take precedence over ordinary affinity.

`channel_affinity_source` supports `auto`, `header`, `session_id`, `prompt_cache_key`
and legacy `api_key`. Auto checks the configured header (`X-Session-Id` by default),
body `session_id` (also `metadata.session_id`), then `prompt_cache_key`. If none is
present it retains legacy API-key affinity. An explicitly selected source with no
value disables ordinary affinity for that request. Multipart Images can supply the
header. WebSocket requests use handshake headers and each response.create body.

Session identifiers are hashed, then namespaced by API key, group and requested
model. Bindings are process-local, expire using `channel_affinity_ttl_seconds`,
and are periodically pruned to a bounded cache. They are not durable session state.
