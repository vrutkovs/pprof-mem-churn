package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/pprof/profile"
)

func fixture(t *testing.T, types []string, id uint64, name string, values []int64, inline bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "heap.pprof")
	f := &profile.Function{ID: id, Name: name, Filename: "app.go", StartLine: 10}
	loc := &profile.Location{ID: id, Line: []profile.Line{{Function: f, Line: 12}}}
	functions := []*profile.Function{f}
	if inline {
		inner := &profile.Function{ID: id + 1, Name: "inlined", Filename: "helper.go", StartLine: 20}
		loc.Line = append(loc.Line, profile.Line{Function: inner, Line: 22})
		functions = append(functions, inner)
	}
	var sampleTypes []*profile.ValueType
	for _, typ := range types {
		unit := "bytes"
		if strings.HasSuffix(typ, "objects") {
			unit = "count"
		}
		sampleTypes = append(sampleTypes, &profile.ValueType{Type: typ, Unit: unit})
	}
	p := &profile.Profile{SampleType: sampleTypes, Sample: []*profile.Sample{{Location: []*profile.Location{loc}, Value: values}}, Location: []*profile.Location{loc}, Function: functions, PeriodType: &profile.ValueType{Type: "space", Unit: "bytes"}, Period: 524288}
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Write(out); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func input(path string, second int) SnapshotInput {
	return SnapshotInput{Path: path, Time: time.Date(2026, 1, 1, 0, 0, second, 0, time.UTC), Host: "host", PID: 42, Start: "2026-01-01T00:00:00Z", Build: "build-1", GoVersion: "go1.26", MemProfileRate: 524288, GC: true}
}

func TestCompareMatchesReorderedIDsAndTracksChurn(t *testing.T) {
	types := []string{"inuse_space", "inuse_objects", "alloc_space", "alloc_objects"}
	a := input(fixture(t, types, 1, "main.cache", []int64{100, 2, 200, 4}, true), 1)
	b := input(fixture(t, types, 99, "main.cache", []int64{150, 3, 350, 7}, true), 2)
	c := input(fixture(t, types, 5, "main.cache", []int64{100, 2, 500, 10}, true), 3)
	r, err := compare(Manifest{Snapshots: []SnapshotInput{a, b, c}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sites) != 1 {
		t.Fatalf("sites = %d", len(r.Sites))
	}
	s := r.Sites[0]
	if !strings.Contains(s.Stack, "inlined") || s.PeakBytes == nil || *s.PeakBytes != 150 || s.NetBytes == nil || *s.NetBytes != 0 || s.NetObjects == nil || *s.NetObjects != 0 || s.Stable || *s.Points[1].DeltaBytes != 50 || *s.Points[1].DeltaObjects != 1 || *s.Points[2].DeltaBytes != -50 || *s.Points[2].DeltaObjects != -1 || *s.Points[2].ChurnBytes != 150 || *s.Points[2].ChurnObjects != 3 {
		t.Fatalf("site: %+v", s)
	}
}

func TestCompareMissingSitesTypesAndNegativeChurn(t *testing.T) {
	types := []string{"inuse_space", "inuse_objects", "alloc_space", "alloc_objects"}
	a := input(fixture(t, types, 1, "first", []int64{50, 1, 200, 4}, false), 1)
	b := input(fixture(t, []string{"inuse_space"}, 2, "second", []int64{80}, false), 2)
	c := input(fixture(t, types, 3, "first", []int64{60, 2, 100, 2}, false), 3)
	r, err := compare(Manifest{Snapshots: []SnapshotInput{a, b, c}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sites) != 2 || len(r.Warnings) == 0 {
		t.Fatalf("report: %+v", r)
	}
	var first *Site
	for i := range r.Sites {
		if strings.Contains(r.Sites[i].Stack, "first") {
			first = &r.Sites[i]
		}
	}
	if first == nil || first.Points[1].Present || first.Points[1].ChurnBytes != nil || first.Points[2].ChurnBytes != nil {
		t.Fatalf("missing/negative churn: %+v", first)
	}
	for _, site := range r.Sites {
		if strings.Contains(site.Stack, "second") && site.Points[1].RetainedObjects != nil {
			t.Fatal("missing retained object samples misreported as zero")
		}
	}
}

func TestCompareRejectsIncompatibleAndUnordered(t *testing.T) {
	types := []string{"inuse_space"}
	a := input(fixture(t, types, 1, "site", []int64{1}, false), 1)
	b := input(fixture(t, types, 2, "site", []int64{2}, false), 2)
	for _, change := range []struct {
		name string
		edit func(*SnapshotInput)
	}{
		{"process", func(s *SnapshotInput) { s.PID++ }},
		{"start", func(s *SnapshotInput) { s.Start = "other" }},
		{"build", func(s *SnapshotInput) { s.Build = "other" }},
		{"rate", func(s *SnapshotInput) { s.MemProfileRate++ }},
		{"gc", func(s *SnapshotInput) { s.GC = false }},
		{"order", func(s *SnapshotInput) { s.Time = a.Time }},
	} {
		t.Run(change.name, func(t *testing.T) {
			copy := b
			change.edit(&copy)
			if _, err := compare(Manifest{Snapshots: []SnapshotInput{a, copy}}); err == nil {
				t.Fatal("expected incompatibility")
			}
		})
	}
}

func TestUnresolvedStacksRemainDistinct(t *testing.T) {
	a := input(fixture(t, []string{"inuse_space"}, 1, "", []int64{1}, false), 1)
	b := input(fixture(t, []string{"inuse_space"}, 2, "", []int64{2}, false), 2)
	r, err := compare(Manifest{Snapshots: []SnapshotInput{a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) == 0 {
		t.Fatal("missing attribution warning")
	}
	if len(r.Sites) != 2 {
		t.Fatalf("unresolved zero-address sites merged: %d", len(r.Sites))
	}
}

func TestUnresolvedMappingBuildsRemainDistinct(t *testing.T) {
	a := &profile.Sample{Location: []*profile.Location{{Address: 4096, Mapping: &profile.Mapping{BuildID: "library-a"}}}}
	b := &profile.Sample{Location: []*profile.Location{{Address: 4096, Mapping: &profile.Mapping{BuildID: "library-b"}}}}
	first, _ := stackKey(a, "app", "one")
	second, _ := stackKey(b, "app", "two")
	if first == second {
		t.Fatal("same address in different mappings merged")
	}
}
