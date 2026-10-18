package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func orderSites(sites []Site, key string) ([]Site, error) {
	if key != "latest" && key != "growth" && key != "churn" {
		return nil, fmt.Errorf("invalid sort %q (use latest, growth, churn)", key)
	}
	out := append([]Site(nil), sites...)
	sort.Slice(out, func(i, j int) bool {
		value := func(s Site) int64 {
			switch key {
			case "growth":
				if s.NetBytes != nil {
					return *s.NetBytes
				}
				return -1 << 63
			case "churn":
				if p := s.Points[len(s.Points)-1].ChurnBytes; p != nil {
					return *p
				}
				return -1
			default:
				if p := s.Points[len(s.Points)-1].RetainedBytes; p != nil {
					return *p
				}
				return -1
			}
		}
		if value(out[i]) == value(out[j]) {
			return out[i].Stack < out[j].Stack
		}
		return value(out[i]) > value(out[j])
	})
	return out, nil
}

func signed(n *int64) string {
	if n == nil {
		return "unknown"
	}
	if *n > 0 {
		return fmt.Sprintf("+%d", *n)
	}
	return fmt.Sprintf("%d", *n)
}

func value(n *int64) string {
	if n == nil {
		return "unknown"
	}
	return fmt.Sprint(*n)
}

func grouped(n *int64) string {
	if n == nil {
		return "unknown"
	}
	digits := fmt.Sprint(*n)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var result strings.Builder
	result.WriteString(sign)
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteRune(digit)
	}
	return result.String()
}

func groupedSigned(n *int64) string {
	if n != nil && *n > 0 {
		return "+" + grouped(n)
	}
	return grouped(n)
}

func humanBytes(n *int64) string {
	if n == nil {
		return "unknown"
	}
	const binaryUnit = 1024
	units := []string{"bytes", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	amount := math.Abs(float64(*n))
	unit := 0
	for amount >= binaryUnit && unit < len(units)-1 {
		amount /= binaryUnit
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d bytes", *n)
	}
	sign := ""
	if *n < 0 {
		sign = "-"
	}
	if amount == math.Trunc(amount) {
		return fmt.Sprintf("%s%.0f %s", sign, amount, units[unit])
	}
	return fmt.Sprintf("%s%.1f %s", sign, amount, units[unit])
}

func humanSigned(n *int64) string {
	if n != nil && *n > 0 {
		return "+" + humanBytes(n)
	}
	return humanBytes(n)
}

func cellClass(n *int64) string {
	if n == nil {
		return "unknown"
	}
	if *n > 0 {
		return "growth"
	}
	if *n < 0 {
		return "shrinkage"
	}
	return "no-change"
}

func cellLabel(n *int64) string {
	if n == nil {
		return "unknown change"
	}
	if *n > 0 {
		return "growth"
	}
	if *n < 0 {
		return "shrinkage"
	}
	return "no change"
}

type callFrame struct {
	Name   string
	Source string
	Full   string
}

func shortPath(path string, parts int) string {
	segments := strings.Split(path, "/")
	if len(segments) > parts {
		segments = segments[len(segments)-parts:]
	}
	return strings.Join(segments, "/")
}

func stackFrames(stack string) []callFrame {
	parts := strings.Split(stack, " ← ")
	frames := []callFrame{}
	for i := len(parts) - 1; i > 0; i-- {
		if strings.HasPrefix(parts[i], "mapping:") {
			continue
		}
		name, source, _ := strings.Cut(parts[i], " (")
		frames = append(frames, callFrame{shortPath(name, 1), shortPath(strings.TrimSuffix(source, ")"), 3), parts[i]})
	}
	if len(frames) == 0 {
		return []callFrame{{Name: stack, Full: stack}}
	}
	return frames
}

func siteLabel(stack string) string {
	frames := stackFrames(stack)
	return frames[len(frames)-1].Name
}

type origin struct {
	Label string
	Value string
	Full  string
}

func stackOrigins(stack string) []origin {
	parts := strings.Split(stack, " ← ")
	result := []origin{{Label: "Build", Value: parts[0], Full: parts[0]}}
	if len(result[0].Value) > 12 {
		result[0].Value = result[0].Value[:12]
	}
	seen := map[string]bool{}
	for _, part := range parts[1:] {
		if !strings.HasPrefix(part, "mapping:") || seen[part] {
			continue
		}
		seen[part] = true
		identity, file, _ := strings.Cut(strings.TrimPrefix(part, "mapping:"), ":")
		if len(identity) > 12 {
			identity = identity[:12]
		}
		result = append(result, origin{Label: "Mapping", Value: fmt.Sprintf("%s (%s)", file, identity), Full: part})
	}
	return result
}

func percent(current, peak *int64) int {
	if current == nil || peak == nil || *peak <= 0 || *current <= 0 {
		return 0
	}
	if *current >= *peak {
		return 100
	}
	return int(float64(*current) / float64(*peak) * 100)
}

//go:embed report.tmpl
var reportTemplate string

var heatmap = template.Must(template.New("heatmap").Funcs(template.FuncMap{
	"signed": signed, "value": value, "class": cellClass, "label": cellLabel,
	"siteLabel": siteLabel, "percent": percent,
	"grouped": grouped, "groupedSigned": groupedSigned,
	"humanBytes": humanBytes, "humanSigned": humanSigned,
	"stackFrames": stackFrames, "stackOrigins": stackOrigins,
	"add": func(a, b int) int { return a + b },
}).Parse(reportTemplate))

func renderHTML(w io.Writer, r Report, sortBy string, limit int) error {
	sites, err := orderSites(r.Sites, sortBy)
	if err != nil {
		return err
	}
	if limit < 1 {
		return fmt.Errorf("limit must be positive")
	}
	total := len(sites)
	if len(sites) > limit {
		sites = sites[:limit]
	}
	return heatmap.Execute(w, struct {
		Snapshots []SnapshotInput
		Sites     []Site
		Warnings  []string
		Sort      string
		Total     int
	}{r.Snapshots, sites, r.Warnings, sortBy, total})
}

func printTable(w io.Writer, r Report, limit int) {
	for _, category := range []struct{ title, key string }{{"biggest retained memory", "latest"}, {"largest growth", "growth"}, {"largest shrinkage", "shrink"}, {"highest allocation churn (latest interval)", "churn"}} {
		sites := append([]Site(nil), r.Sites...)
		if category.key == "shrink" {
			sort.Slice(sites, func(i, j int) bool {
				if sites[i].NetBytes == nil {
					return false
				}
				if sites[j].NetBytes == nil {
					return true
				}
				return *sites[i].NetBytes < *sites[j].NetBytes
			})
		} else {
			sites, _ = orderSites(sites, category.key)
		}
		fmt.Fprintln(w, category.title+":")
		shown := 0
		for _, s := range sites {
			if category.key == "shrink" && (s.NetBytes == nil || *s.NetBytes >= 0) {
				break
			}
			if category.key == "growth" && (s.NetBytes == nil || *s.NetBytes <= 0) {
				break
			}
			if shown >= limit {
				break
			}
			churn := s.TotalChurnBytes
			if category.key == "churn" {
				churn = s.Points[len(s.Points)-1].ChurnBytes
			}
			fmt.Fprintf(w, "  retained memory %s | change %s | allocation churn %s | %s\n", humanBytes(s.Points[len(s.Points)-1].RetainedBytes), humanSigned(s.NetBytes), humanBytes(churn), siteLabel(s.Stack))
			shown++
		}
		if shown == 0 {
			fmt.Fprintln(w, "  none")
		}
	}
	for _, warning := range r.Warnings {
		fmt.Fprintln(w, "warning:", warning)
	}
}

func run(manifestPath, jsonPath, htmlPath, sortBy string, limit int, stdout io.Writer) error {
	if limit < 1 {
		return fmt.Errorf("limit must be positive")
	}
	if _, err := orderSites(nil, sortBy); err != nil {
		return err
	}
	if manifestPath == "" || jsonPath == "" || htmlPath == "" || jsonPath == htmlPath {
		return fmt.Errorf("provide distinct manifest, JSON and HTML paths")
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	for i := range m.Snapshots {
		if !filepath.IsAbs(m.Snapshots[i].Path) {
			m.Snapshots[i].Path = filepath.Join(filepath.Dir(manifestPath), m.Snapshots[i].Path)
		}
	}
	inputs := []string{manifestPath}
	for _, snap := range m.Snapshots {
		inputs = append(inputs, snap.Path)
	}
	for _, output := range []string{jsonPath, htmlPath} {
		for _, input := range inputs {
			if samePath(output, input) {
				return fmt.Errorf("output %s would overwrite input %s", output, input)
			}
		}
	}
	if samePath(jsonPath, htmlPath) {
		return fmt.Errorf("JSON and HTML outputs must differ")
	}
	r, err := compare(m)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(jsonPath, append(encoded, '\n'), 0644); err != nil {
		return err
	}
	f, err := os.Create(htmlPath)
	if err != nil {
		return err
	}
	err = renderHTML(f, r, sortBy, limit)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	printTable(stdout, r, limit)
	return nil
}

func samePath(a, b string) bool {
	path := func(p string) string {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		absolute, _ := filepath.Abs(p)
		return absolute
	}
	return path(a) == path(b)
}
