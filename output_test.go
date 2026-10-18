package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHeatmapEscapesAndLabelsCells(t *testing.T) {
	r := Report{Snapshots: []SnapshotInput{{}, {}, {}}, Sites: []Site{{Stack: `<script>alert("x")</script>`, Points: []Point{
		{Present: true, RetainedBytes: number(0), RetainedObjects: number(0)},
		{Present: true, RetainedBytes: number(100), DeltaBytes: number(100), RetainedObjects: number(2)},
		{Present: true, RetainedBytes: number(0), DeltaBytes: number(-100), RetainedObjects: number(0)},
	}}, {Stack: "stable", Stable: true, Points: []Point{
		{Present: true, RetainedBytes: number(10)}, {Present: true, RetainedBytes: number(10), DeltaBytes: number(0)}, {Present: true, RetainedBytes: number(10), DeltaBytes: number(0)},
	}}}}
	var out bytes.Buffer
	if err := renderHTML(&out, r, "latest", 10); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{"&lt;script&gt;", "growth", "shrinkage", "no change", "&#43;100", "-100", "retained memory", "retained objects", "allocation churn", "aria-label", "<td"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(html, `<script>alert`) {
		t.Fatal("unescaped symbol")
	}
}

func TestEndToEndThreeSnapshots(t *testing.T) {
	types := []string{"inuse_space", "inuse_objects", "alloc_space", "alloc_objects"}
	a := input(fixture(t, types, 1, "main.app", []int64{100, 1, 200, 2}, false), 1)
	b := input(fixture(t, types[:3], 2, "main.app", []int64{150, 2, 400}, false), 2)
	c := input(fixture(t, types, 3, "main.app", []int64{125, 1, 600, 6}, false), 3)
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	data, _ := json.Marshal(Manifest{Snapshots: []SnapshotInput{a, b, c}})
	if err := os.WriteFile(manifest, data, 0600); err != nil {
		t.Fatal(err)
	}
	var table bytes.Buffer
	if err := run(manifest, filepath.Join(dir, "out.json"), filepath.Join(dir, "out.html"), "growth", 1, &table); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(table.String(), "25") || !strings.Contains(table.String(), "allocation churn") {
		t.Fatalf("table: %s", table.String())
	}
	jsonData, err := os.ReadFile(filepath.Join(dir, "out.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(jsonData, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Sites) != 1 || report.Sites[0].NetBytes == nil || *report.Sites[0].NetBytes != 25 || report.Sites[0].TotalChurnBytes == nil || *report.Sites[0].TotalChurnBytes != 400 {
		t.Fatalf("json: %+v", report.Sites)
	}
	if len(report.Warnings) == 0 || !strings.Contains(table.String(), "missing alloc_objects") {
		t.Fatal("missing table/JSON warning")
	}
	html, err := os.ReadFile(filepath.Join(dir, "out.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "125") || !strings.Contains(string(html), "&#43;50") || !strings.Contains(string(html), "-25") {
		t.Fatalf("html mismatch: %s", html)
	}
	if !strings.Contains(string(html), "missing alloc_objects") {
		t.Fatal("missing HTML warning")
	}
}

func TestGoApplicationProfiles(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("go", "run", "./examples/demo", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("demo: %v: %s", err, out)
	}
	var table bytes.Buffer
	jsonPath, htmlPath := filepath.Join(dir, "out.json"), filepath.Join(dir, "out.html")
	if err := run(filepath.Join(dir, "manifest.json"), jsonPath, htmlPath, "latest", 10, &table); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Snapshots) != 3 || len(r.Sites) == 0 || !strings.Contains(table.String(), "retained memory") {
		t.Fatalf("report: %s, sites=%d", table.String(), len(r.Sites))
	}
}

func TestSortByLatestIntervalChurn(t *testing.T) {
	sites := []Site{
		{Stack: "old", Points: []Point{{}, {ChurnBytes: number(100)}, {ChurnBytes: number(1)}}, TotalChurnBytes: number(101)},
		{Stack: "recent", Points: []Point{{}, {ChurnBytes: number(1)}, {ChurnBytes: number(20)}}, TotalChurnBytes: number(21)},
	}
	sorted, err := orderSites(sites, "churn")
	if err != nil {
		t.Fatal(err)
	}
	if sorted[0].Stack != "recent" {
		t.Fatalf("sorted by total rather than latest interval: %+v", sorted)
	}
}

func TestRunDoesNotOverwriteInput(t *testing.T) {
	types := []string{"inuse_space"}
	a := input(fixture(t, types, 1, "site", []int64{1}, false), 1)
	b := input(fixture(t, types, 2, "site", []int64{2}, false), 2)
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest.json")
	data, _ := json.Marshal(Manifest{Snapshots: []SnapshotInput{a, b}})
	if err := os.WriteFile(manifest, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(manifest, a.Path, filepath.Join(dir, "out.html"), "latest", 1, &bytes.Buffer{}); err == nil {
		t.Fatal("overwrote profile")
	}
	if err := run(manifest, filepath.Join(dir, "out.json"), manifest, "latest", 1, &bytes.Buffer{}); err == nil {
		t.Fatal("overwrote manifest")
	}
}
