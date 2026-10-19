# pprof-mem-churn

Compare heap profiles from one Go process. CLI prints ranked allocation sites and writes JSON plus a self-contained HTML heatmap.

## Run demo

```sh
mkdir -p /home/vrutkovs/tmp/opencode/pprof-mem-churn-demo
go run ./examples/demo /home/vrutkovs/tmp/opencode/pprof-mem-churn-demo
go run . -manifest /home/vrutkovs/tmp/opencode/pprof-mem-churn-demo/manifest.json -json /home/vrutkovs/tmp/opencode/pprof-mem-churn-demo/report.json -html /home/vrutkovs/tmp/opencode/pprof-mem-churn-demo/report.html
go test ./...
```

## Input

Use a JSON manifest with at least two ordered snapshots. Paths can be relative to the manifest. Example:

```json
{
  "snapshots": [
    {"path":"heap-1.pprof","time":"2026-01-01T12:00:00Z","host":"server-1","pid":123,"start":"2026-01-01T11:00:00Z","build":"binary-sha256","go_version":"go1.26.8","mem_profile_rate":524288,"gc":true},
    {"path":"heap-2.pprof","time":"2026-01-01T12:01:00Z","host":"server-1","pid":123,"start":"2026-01-01T11:00:00Z","build":"binary-sha256","go_version":"go1.26.8","mem_profile_rate":524288,"gc":true}
  ]
}
```

Record `host`, PID, process start time, binary build identity, Go version, `runtime.MemProfileRate`, and whether capture forced garbage collection (`gc`). Supply capture times in RFC 3339 format. The CLI checks metadata consistency and profile sampling period; it cannot verify process identity from a heap profile alone. Keep capture settings consistent. `-sort latest|growth|churn` selects heatmap order; `churn` uses the latest interval. `-limit N` limits displayed rows, not JSON sites.

Retained memory and object counts are sampled estimates. Heap profiles reflect allocations accounted for by a completed garbage collection, not an exact inventory. Allocation churn uses differences between cumulative allocation samples; negative differences are warnings, not negative churn. Stable retained estimates do not establish object survival or inactivity. Profiles do not measure memory reads or writes.
