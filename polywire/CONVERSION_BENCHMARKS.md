# Protocol conversion benchmarks

Measured locally with Go 1.26.4, Windows/amd64, AMD Ryzen 9 7940HX. These
benchmarks measure local conversion and allocation costs, excluding network I/O
and provider latency. `B/op` is cumulative allocated memory, not peak live memory.

## Incremental stream aggregation

Each stream contains 2,048 fragments of 32 bytes. The same benchmark fixture was
run before and after replacing retained chunks and repeated string concatenation
with incremental accumulation. The baseline implementation is from `84c6e33`.

| Stream content | Before ns/op | After ns/op | Before B/op | After B/op | Before allocs/op | After allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Text | 15,113,211 | 82,567 | 72,281,922 | 287,072 | 4,117 | 29 |
| Reasoning | 19,476,438 | 79,413 | 72,248,464 | 287,072 | 2,068 | 29 |
| Tool arguments | 15,639,536 | 123,669 | 72,248,600 | 287,545 | 2,068 | 33 |
| Indexed text block | 14,533,113 | 137,844 | 72,281,411 | 288,113 | 4,117 | 39 |

The benchmark also includes 128-fragment streams to expose scaling. Input chunks
are allocated before timing; each operation includes accumulation and
`BuildAndReset`. Snapshot isolation, mixed content, citations, parallel tools,
refusals and finalization behavior are checked separately by regression tests.

```sh
go test ./model -run '^$' -bench '^BenchmarkStreamAggregator$' -benchmem -benchtime=200ms -count=1
```

## Reusing a validated request

Both paths convert an OpenAI Chat request to Anthropic using the same code
revision. The baseline calls `PlanRequestForModel` and then `BuildRequest`;
the reuse path calls `PrepareRequestForModelWithConfig` and then
`PreparedRequest.Build`. Each operation includes planning, report generation and
HTTP request construction. Values below are medians of three runs.

| Input text | Rebuild ns/op | Reuse ns/op | Rebuild B/op | Reuse B/op | Rebuild allocs/op | Reuse allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 128 bytes | 44,880 | 26,319 | 48,895 | 32,417 | 332 | 196 |
| 1 MiB | 36,700,133 | 18,138,367 | 22,718,193 | 11,109,109 | 408 | 233 |

This comparison does not include additional savings from reusing the same
prepared body across retries. Octopus bounds retained prepared bodies to eight
entries and 16 MiB per relay request; uncached candidates use ordinary builds.

```sh
go test ./outbound -run '^$' -bench '^BenchmarkPlanAndBuildRequest$' -benchmem -benchtime=300ms -count=3
```

Run commands from `polywire/`. Results vary with Go version, CPU, GC and payload;
the fixtures and commands make the comparisons repeatable on the target host.
