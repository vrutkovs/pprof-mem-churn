package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	manifest := flag.String("manifest", "", "JSON manifest with ordered snapshots and run metadata")
	jsonPath := flag.String("json", "report.json", "JSON output path")
	htmlPath := flag.String("html", "report.html", "self-contained HTML output path")
	sortBy := flag.String("sort", "latest", "heatmap sort: latest, growth, churn")
	limit := flag.Int("limit", 30, "visible rows per table and heatmap")
	flag.Parse()
	if err := run(*manifest, *jsonPath, *htmlPath, *sortBy, *limit, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "pprof-mem-map:", err)
		os.Exit(1)
	}
}
