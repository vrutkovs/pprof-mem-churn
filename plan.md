# Compare Go heap pprof snapshots

## Goal

Build an offline CLI that compares multiple heap pprof snapshots from one Go process. Show which allocation sites grow, shrink, or stay stable. Report estimated retained bytes and objects, allocation churn, and a visual heatmap. No application source changes required.

An **allocation site** is the stack responsible for sampled allocations. Site stability does not mean the same objects survived. Heap pprof has no object addresses or memory-access history, so the tool cannot classify memory as cold or warm.

## Inputs

- Accept an ordered list of heap profiles with capture times, or a manifest containing those paths and times. Record process identity (host PID and start time), binary/build identity, Go version, and `runtime.MemProfileRate` when available. Require enough metadata to establish that profiles belong to one compatible run before computing churn; do not silently merge different runs.
- Read profiles with `github.com/google/pprof/profile`. Use `inuse_space`, `inuse_objects`, `alloc_space`, and `alloc_objects` when present. Report missing sample types instead of treating them as zero.
- Keep heap capture settings consistent. HTTP pprof `?gc=1` forces a garbage collection; record whether it was used. Heap profiles contain sampling estimates and reflect allocations accounted for by a completed garbage collection, not an exact current object inventory.

## Comparison

1. Group samples by normalized allocation stack using build identity, function names, source locations, and inline frames. Do not compare pprof location IDs across snapshots. Keep unresolved locations distinct where possible; show attribution gaps rather than merging unrelated unknown sites.
2. For each site and snapshot, report estimated retained bytes (`inuse_space`) and retained objects (`inuse_objects`). Show adjacent and first-to-last changes, peak retained bytes, and which snapshots contain the site.
3. Compute **allocation churn** from adjacent differences in cumulative `alloc_space` and `alloc_objects`, only for profiles from the same compatible process. Flag negative differences or missing cumulative samples; never display negative churn. High churn with stable retained bytes can indicate many short-lived allocations and possible garbage-collection cost. Churn is not access frequency.
4. Label a site **stable** only when its estimated retained bytes do not change between compared snapshots. This does not prove object survival or inactivity. Do not compute average object size.

## Output

- Print a short table of biggest retained sites, largest growth and shrinkage, and highest allocation churn. Write JSON with per-site time series, metadata, and warnings.
- Generate a self-contained HTML heatmap. Rows represent allocation sites; ordered columns represent snapshots. Cells show retained bytes and change from the preceding snapshot. Use distinct colors for growth and shrinkage, neutral styling for no measured change, and an explicit legend. Include numeric values and accessible labels so color is not the only signal.
- Allow sorting by latest retained bytes, net growth, or interval churn. Limit visible rows without dropping sites from JSON. Show retained object counts in row details. Never sum retained bytes across snapshots.
- Use `retained memory` and `allocation churn` as labels. Do not use `hot`, `warm`, or `cold` for pprof-derived values.

## Tests

1. Unit-test profile parsing and stack matching with reordered location IDs, inline frames, missing symbols, absent sites, missing sample types, incompatible processes/builds, sampling-rate changes, out-of-order times, and negative cumulative deltas.
2. Test heatmap cells and legend for zero, growth, shrinkage, and stable estimates. Escape profile-supplied symbol names in HTML. Verify values remain readable without color.
3. End-to-end test with at least three snapshots; confirm table, JSON, and HTML show consistent values and warnings.

## Future work: access temperature

Go heap pprof, runtime metrics, and garbage collection do not record reads or writes to retained objects. To estimate cold or warm usage, instrument access points in selected caches or other data structures. Track observed reads and writes over a chosen interval, then display those measurements separately from pprof retention. Host-side memory-access sampling is another option, but mapping sampled addresses back to Go objects requires additional evidence and has sampling and address-reuse limits. Neither path is required for this tool.

## References

- [Go heap profile semantics](https://pkg.go.dev/runtime/pprof#hdr-Heap_profile-Profile)
- [pprof profile format](https://github.com/google/pprof/tree/main/proto)
