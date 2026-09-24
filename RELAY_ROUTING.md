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
