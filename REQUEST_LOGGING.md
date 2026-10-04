# Request recording

Request details distinguish the client's request/response from each upstream
attempt's request/response. These are application-level payloads observed before
upstream conversion and after downstream serialization; they are not a packet
capture. Historical logs without capture metadata retain the previous viewer.

- `relay_log_content_enabled`: capture bodies (default `true`). Disabling history
  also disables body capture. Early rejected requests retain response diagnostics
  without retaining the input body.
- `relay_log_content_keep_period`: body retention in days (default `7`, `0` means
  permanent). Summary retention continues to use `relay_log_keep_period`.
- `relay_log_content_max_mb`: uncompressed limit for each body (default `4`, range
  `1..64` MiB). All attempts share a request budget of four times this limit.

Bodies are gzip-compressed incrementally, stored in `relay_request_contents` and
`relay_attempt_contents`, and excluded from list/detail metadata and live SSE.
The body endpoint loads only one direction of the selected exchange. Size limits,
incomplete reads, disabled capture, queue overflow and expiration remain explicit.
The content queue has an independent 64 MiB budget; body overflow keeps the log
summary and marks the bodies unavailable. Retention runs in batches. Clearing logs
deletes their contents in the same database transactions. Both JSON and ZIP exports
include the content tables; JSON imports restore them.

Headers redact credentials before entering capture buffers, including sensitive
header names and known upstream keys in custom headers. URLs redact credential
query parameters and user information. Payloads themselves retain prompts and tool
outputs. The request ID returned in `X-Octopus-Request-Id` identifies capture from
ingress; the existing numeric log ID is still allocated at persistence admission,
preserving concurrent clear semantics.

Body search covers client and attempted upstream payloads, including new compressed
records. The existing time-window validation applies. Searches are bounded to 500
candidate requests and 128 MiB of decoded content per call; cursor searches expose
continuation, and overly broad page searches request a narrower filter.

SSE bodies include a bounded index of up to 256 events, using protocol line
boundaries (including CRLF split across reads), byte offsets and elapsed times
from the start of that message. Incomplete final events and index overflow are
explicit. Stream completion is separate from byte-capture completeness.

Each WebSocket response.create is recorded separately. The original client
message, final upstream message and actual received/delivered messages are kept
independently, including upstream error messages before classification. WS
bodies concatenate raw message bytes; the event index preserves message
boundaries and text/binary type. They have no invented per-message HTTP status.
Handshake headers are not recorded for pooled WS connections.

Images and Responses compact use the same request/attempt storage and retain
final local errors. Multipart requests store a prepared-form description with
ordered fields and file names, MIME types and sizes; uploaded file bytes are
omitted. The upstream description reflects model mapping, and is explicitly
not proof of complete upload. It is bounded to 128 parts and 16 KiB per field.
JSON image responses, including base64 image data, follow the regular body
limits. Event and multipart representations are labeled in the viewer.
