package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/google/pprof/profile"
)

type SnapshotInput struct {
	Path           string    `json:"path"`
	Time           time.Time `json:"time"`
	Host           string    `json:"host"`
	PID            int       `json:"pid"`
	Start          string    `json:"start"`
	Build          string    `json:"build"`
	GoVersion      string    `json:"go_version"`
	MemProfileRate int64     `json:"mem_profile_rate"`
	GC             bool      `json:"gc"`
}

type Manifest struct {
	Snapshots []SnapshotInput `json:"snapshots"`
}

type Point struct {
	Present         bool   `json:"present"`
	RetainedBytes   *int64 `json:"retained_bytes"`
	RetainedObjects *int64 `json:"retained_objects"`
	DeltaBytes      *int64 `json:"delta_bytes"`
	DeltaObjects    *int64 `json:"delta_objects"`
	ChurnBytes      *int64 `json:"churn_bytes"`
	ChurnObjects    *int64 `json:"churn_objects"`
}

type Site struct {
	Stack           string  `json:"stack"`
	Points          []Point `json:"points"`
	PeakBytes       *int64  `json:"peak_bytes"`
	NetBytes        *int64  `json:"net_bytes"`
	NetObjects      *int64  `json:"net_objects"`
	Stable          bool    `json:"stable"`
	TotalChurnBytes *int64  `json:"total_churn_bytes"`
}

type Report struct {
	Snapshots []SnapshotInput `json:"snapshots"`
	Sites     []Site          `json:"sites"`
	Warnings  []string        `json:"warnings"`
}

func number(n int64) *int64 { return &n }

func readProfile(path string) (*profile.Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return profile.Parse(f)
}

func sampleIndex(p *profile.Profile, name string) int {
	for i, typ := range p.SampleType {
		if typ.Type == name {
			return i
		}
	}
	return -1
}

func stackKey(sample *profile.Sample, build, fallback string) (string, bool) {
	parts := []string{build}
	unresolved := false
	for _, loc := range sample.Location {
		if loc.Mapping != nil && (loc.Mapping.BuildID != "" || loc.Mapping.File != "") {
			parts = append(parts, "mapping:"+loc.Mapping.BuildID+":"+loc.Mapping.File)
		}
		if len(loc.Line) == 0 {
			unresolved = true
			parts = append(parts, unresolvedKey(loc.Address, fallback))
			continue
		}
		for _, line := range loc.Line {
			if line.Function == nil || line.Function.Name == "" {
				unresolved = true
				parts = append(parts, fmt.Sprintf("%s:%d", unresolvedKey(loc.Address, fallback), line.Line))
				continue
			}
			parts = append(parts, fmt.Sprintf("%s (%s:%d)", line.Function.Name, line.Function.Filename, line.Line))
		}
	}
	return strings.Join(parts, " ← "), unresolved
}

func unresolvedKey(address uint64, fallback string) string {
	if address == 0 {
		return "unresolved@" + fallback
	}
	return fmt.Sprintf("unresolved@%#x", address)
}

func compare(m Manifest) (Report, error) {
	r := Report{Snapshots: m.Snapshots, Sites: []Site{}, Warnings: []string{}}
	if len(m.Snapshots) < 2 {
		return r, fmt.Errorf("need at least two snapshots")
	}
	first := m.Snapshots[0]
	if first.Host == "" || first.PID <= 0 || first.Start == "" || first.Build == "" || first.GoVersion == "" || first.MemProfileRate <= 0 || first.Time.IsZero() {
		return r, fmt.Errorf("snapshot 1: incomplete run metadata")
	}
	for i, snap := range m.Snapshots {
		if snap.Path == "" || snap.Time.IsZero() || (i > 0 && !snap.Time.After(m.Snapshots[i-1].Time)) {
			return r, fmt.Errorf("snapshot %d: missing path/time or unordered time", i+1)
		}
		if snap.Host != first.Host || snap.PID != first.PID || snap.Start != first.Start || snap.Build != first.Build || snap.GoVersion != first.GoVersion || snap.MemProfileRate != first.MemProfileRate || snap.GC != first.GC {
			return r, fmt.Errorf("snapshot %d: incompatible run or capture settings", i+1)
		}
	}

	sites := map[string]*Site{}
	available := make([]map[string]bool, len(m.Snapshots))
	counters := make(map[string][]map[string]int64)
	for i, snap := range m.Snapshots {
		p, err := readProfile(snap.Path)
		if err != nil {
			return r, fmt.Errorf("snapshot %d: %w", i+1, err)
		}
		if p.Period > 0 && p.Period != snap.MemProfileRate {
			return r, fmt.Errorf("snapshot %d: sampling rate differs from profile", i+1)
		}
		available[i] = map[string]bool{}
		indices := map[string]int{}
		for _, typ := range []string{"inuse_space", "inuse_objects", "alloc_space", "alloc_objects"} {
			indices[typ] = sampleIndex(p, typ)
			available[i][typ] = indices[typ] >= 0
			if indices[typ] < 0 {
				r.Warnings = append(r.Warnings, fmt.Sprintf("snapshot %d: missing %s", i+1, typ))
			}
		}
		for sampleIndex, sample := range p.Sample {
			key, unresolved := stackKey(sample, snap.Build, fmt.Sprintf("snapshot-%d/sample-%d", i+1, sampleIndex+1))
			if unresolved {
				r.Warnings = append(r.Warnings, fmt.Sprintf("snapshot %d: unresolved location in %s", i+1, key))
			}
			s := sites[key]
			if s == nil {
				s = &Site{Stack: key, Points: make([]Point, len(m.Snapshots))}
				sites[key] = s
				counters[key] = make([]map[string]int64, len(m.Snapshots))
			}
			if counters[key][i] == nil {
				counters[key][i] = map[string]int64{}
			}
			for _, typ := range []string{"alloc_space", "alloc_objects"} {
				if idx := indices[typ]; idx >= 0 {
					counters[key][i][typ] += sample.Value[idx]
				}
			}
			pt := &s.Points[i]
			pt.Present = true
			for _, field := range []struct {
				name   string
				target **int64
			}{
				{"inuse_space", &pt.RetainedBytes}, {"inuse_objects", &pt.RetainedObjects},
			} {
				if idx := indices[field.name]; idx >= 0 {
					if *field.target == nil {
						*field.target = number(0)
					}
					**field.target += sample.Value[idx]
				}
			}
		}
	}
	// Cumulative counters measure churn; retained values do not.
	for _, s := range sites {
		cumulative := counters[s.Stack]
		churnComplete := true
		for i := range s.Points {
			pt := &s.Points[i]
			if !pt.Present {
				if available[i]["inuse_space"] {
					pt.RetainedBytes = number(0)
				}
				if available[i]["inuse_objects"] {
					pt.RetainedObjects = number(0)
				}
			}
			if pt.RetainedBytes != nil && (s.PeakBytes == nil || *pt.RetainedBytes > *s.PeakBytes) {
				s.PeakBytes = number(*pt.RetainedBytes)
			}
			if i == 0 {
				continue
			}
			prev := s.Points[i-1]
			if pt.RetainedBytes != nil && prev.RetainedBytes != nil {
				pt.DeltaBytes = number(*pt.RetainedBytes - *prev.RetainedBytes)
			}
			if pt.RetainedObjects != nil && prev.RetainedObjects != nil {
				pt.DeltaObjects = number(*pt.RetainedObjects - *prev.RetainedObjects)
			}
			for _, field := range []struct {
				name   string
				target **int64
			}{{"alloc_space", &pt.ChurnBytes}, {"alloc_objects", &pt.ChurnObjects}} {
				if !available[i][field.name] || !available[i-1][field.name] {
					if field.name == "alloc_space" {
						churnComplete = false
					}
					continue
				}
				delta := cumulative[i][field.name] - cumulative[i-1][field.name]
				if delta < 0 {
					if field.name == "alloc_space" {
						churnComplete = false
					}
					r.Warnings = append(r.Warnings, fmt.Sprintf("snapshot %d: negative %s delta for %s", i+1, field.name, s.Stack))
					continue
				}
				*field.target = number(delta)
				if field.name == "alloc_space" {
					if s.TotalChurnBytes == nil {
						s.TotalChurnBytes = number(0)
					}
					*s.TotalChurnBytes += delta
				}
			}
		}
		start, end := s.Points[0].RetainedBytes, s.Points[len(s.Points)-1].RetainedBytes
		if start != nil && end != nil {
			s.NetBytes = number(*end - *start)
		}
		firstObjects, lastObjects := s.Points[0].RetainedObjects, s.Points[len(s.Points)-1].RetainedObjects
		if firstObjects != nil && lastObjects != nil {
			s.NetObjects = number(*lastObjects - *firstObjects)
		}
		if !churnComplete {
			s.TotalChurnBytes = nil
		}
		s.Stable = start != nil
		for _, pt := range s.Points {
			if pt.RetainedBytes == nil || (start != nil && *pt.RetainedBytes != *start) {
				s.Stable = false
			}
		}
		r.Sites = append(r.Sites, *s)
	}
	sort.Slice(r.Sites, func(i, j int) bool { return r.Sites[i].Stack < r.Sites[j].Stack })
	return r, nil
}
