# SSE decoder benchmarks

Measured on 2026-09-11 with Go 1.26.4, Windows/amd64, AMD Ryzen 9 7940HX.
The baseline is commit `a31d7ae` with only the benchmark harness added. The
updated results use the shared bounded decoder in this change. Each cell is the
median of three runs, with `-cpu=2` and 150 ms per sample.

```powershell
go test ./internal/relay/stream -run '^$' -bench '^BenchmarkSSEContractDecode$' -benchmem -benchtime=150ms -count=3 -cpu=2
```

| Workload | Events per operation | Read/chunk size | Payload |
| --- | ---: | ---: | --- |
| SmallChunks | 64 | 7 bytes | One-character text delta |
| LargeImage | 1 | 4,096 bytes | 1 MiB base64 image field |
| ToolArguments | 256 | 256 bytes | Function argument deltas |
| HighFrequencyText | 1,024 | 4,096 bytes | Short text deltas |

Both paths retain event type, persistent ID and data. Source includes the
background reader, delivery channel and concurrent-close lifecycle. Observer
feeds identical bytes directly into the incremental observer. These benchmarks
measure framing and delivery; they exclude network I/O, provider conversion,
JSON inspection caches, downstream encoding and model generation time.

| Workload / path | Before ns/op | After ns/op | Time change | Before B/op | After B/op | Before allocs/op | After allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| SmallChunks / Source | 72,539 | 55,907 | -22.9% | 18,092 | 9,184 | 206 | 140 |
| SmallChunks / Observer | 23,221 | 15,420 | -33.6% | 14,705 | 4,600 | 444 | 132 |
| LargeImage / Source | 53,596,467 | 721,834 | -98.7% | 7,361,192 | 4,194,966 | 26 | 23 |
| LargeImage / Observer | 3,726,343 | 721,343 | -80.6% | 7,810,671 | 4,190,381 | 29 | 15 |
| ToolArguments / Source | 253,823 | 187,025 | -26.3% | 135,848 | 43,087 | 1,038 | 625 |
| ToolArguments / Observer | 98,834 | 58,539 | -40.8% | 67,537 | 38,504 | 898 | 617 |
| HighFrequencyText / Source | 999,293 | 981,404 | -1.8% | 234,152 | 75,807 | 3,086 | 2,061 |
| HighFrequencyText / Observer | 269,445 | 177,377 | -34.2% | 177,951 | 71,224 | 3,096 | 2,053 |

Every measured median improves, and both allocated bytes and allocation counts
fall for all workloads. The high-frequency Source latency difference is only
1.8%, which is too small to distinguish confidently from scheduling noise in
these short runs. Its allocated bytes fall by 67.6%. The 1 MiB image Source
improves from about 53.6 ms to 0.72 ms; Observer improves from 3.73 ms to 0.72 ms.
These are local microbenchmark results, not an end-to-end latency guarantee.

An intermediate implementation used a 32 KiB reader buffer and a larger minimum
line allocation, which regressed SmallChunks. The final implementation uses a
4 KiB reader buffer, 64-byte minimum line capacity and a short-line scan before
using the standard optimized byte search. Large owned line buffers transfer to
the emitted event instead of being copied through string conversions.

The shared decoder bounds each frame, including comments, metadata and
unfinished lines. It accepts LF, CRLF and CR, a split initial BOM, multiline data
including empty lines, persistent/reset IDs and typed empty terminal events.
The observer previews completed data lines without advancing provider state;
only the final envelope reaches canonical conversion. Valid parsed DTOs are
reused after trailing metadata, while changed data is inspected again. Source
errors do not finalize partial frames. Explicit close remains an error, and one
producer assigns sequences to concurrent consumers.
