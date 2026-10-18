package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"
)

var retained [][]byte

type snapshot struct {
	Path           string    `json:"path"`
	Time           time.Time `json:"time"`
	Host           string    `json:"host"`
	PID            int       `json:"pid"`
	Start          string    `json:"start"`
	Build          string    `json:"build"`
	GoVersion      string    `json:"go_version"`
	MemProfileRate int       `json:"mem_profile_rate"`
	GC             bool      `json:"gc"`
}

func allocate() { retained = append(retained, make([]byte, 256<<10)) }

func capture(dir string, i int, meta snapshot) (snapshot, error) {
	runtime.GC()
	runtime.GC()
	meta.Path = fmt.Sprintf("heap-%d.pprof", i)
	meta.Time = time.Now().UTC()
	f, err := os.Create(filepath.Join(dir, meta.Path))
	if err != nil {
		return meta, err
	}
	err = pprof.Lookup("heap").WriteTo(f, 0)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return meta, err
}

func run(dir string) error {
	runtime.MemProfileRate = 1
	host, err := os.Hostname()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	bin, err := os.ReadFile(executable)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(bin)
	meta := snapshot{Host: host, PID: os.Getpid(), Start: time.Now().UTC().Format(time.RFC3339Nano), Build: hex.EncodeToString(hash[:]), GoVersion: runtime.Version(), MemProfileRate: runtime.MemProfileRate, GC: true}
	var snapshots []snapshot
	for i := 0; i < 3; i++ {
		allocate()
		snap, err := capture(dir, i, meta)
		if err != nil {
			return err
		}
		snapshots = append(snapshots, snap)
	}
	data, err := json.MarshalIndent(struct {
		Snapshots []snapshot `json:"snapshots"`
	}{snapshots}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: demo OUTPUT_DIR")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
