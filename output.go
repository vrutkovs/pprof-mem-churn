package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sort"
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

var heatmap = template.Must(template.New("heatmap").Funcs(template.FuncMap{
	"signed": signed, "value": value, "class": cellClass, "label": cellLabel,
}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Go heap retained memory</title><style>
body{font:1rem system-ui,sans-serif;margin:2rem;color:#202020}table{border-collapse:collapse;width:100%}th,td{border:1px solid #777;padding:.5rem;text-align:right}th:first-child,td:first-child{text-align:left}td{white-space:nowrap}.growth{background:#b8e4cc}.shrinkage{background:#f6c6c0}.no-change{background:#eee}.unknown{background:#fff}.legend span{padding:.3rem;margin-right:1rem}details{white-space:normal}caption{text-align:left;font-weight:bold;margin-bottom:1rem}
</style></head><body><h1>Retained memory by allocation site</h1>
<p>Heap profiles contain sampling estimates, not object identity or access frequency. Stable estimates do not prove object survival or inactivity. Allocation churn measures differences in cumulative allocation samples.</p>
<p class="legend" aria-label="Change legend">Legend: <span class="growth">growth (+)</span><span class="shrinkage">shrinkage (-)</span><span class="no-change">no change (0)</span><span class="unknown">unknown</span></p>
{{if .Warnings}}<h2>Warnings</h2><ul>{{range .Warnings}}<li>{{.}}</li>{{end}}</ul>{{end}}
<table><caption>Retained memory (bytes); showing {{len .Sites}} of {{.Total}} sites. Sorted by {{.Sort}}.</caption><thead><tr><th scope="col">Allocation site</th>{{range .Snapshots}}<th scope="col">{{.Time.Format "2006-01-02 15:04:05Z07:00"}}</th>{{end}}</tr></thead><tbody>
{{range .Sites}}<tr><th scope="row"><details><summary>{{.Stack}}</summary><p>Peak retained memory: {{value .PeakBytes}} bytes. Net change: {{signed .NetBytes}} bytes. Allocation churn: {{value .TotalChurnBytes}} bytes. Stable estimate: {{.Stable}}.</p><p>Retained objects: {{range .Points}}{{value .RetainedObjects}}; {{end}} Net change: {{signed .NetObjects}} objects.</p></details></th>{{range .Points}}<td class="{{class .DeltaBytes}}" aria-label="retained memory {{value .RetainedBytes}} bytes; {{label .DeltaBytes}} {{signed .DeltaBytes}} bytes; retained objects {{value .RetainedObjects}}; object change {{signed .DeltaObjects}}; allocation churn {{value .ChurnBytes}} bytes">{{value .RetainedBytes}} bytes<br>{{if .DeltaBytes}}{{signed .DeltaBytes}} bytes ({{label .DeltaBytes}}){{else}}baseline{{end}}</td>{{end}}</tr>{{end}}
</tbody></table></body></html>`))

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
			fmt.Fprintf(w, "  retained memory %s bytes | change %s | allocation churn %s bytes | %s\n", value(s.Points[len(s.Points)-1].RetainedBytes), signed(s.NetBytes), value(churn), s.Stack)
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
