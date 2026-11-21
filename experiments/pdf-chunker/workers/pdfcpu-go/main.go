// Experimental PDF range chunker for Go WASI Preview 1 (pdfcpu backend).
//
// Contract:
//
//	chunker original.pdf first last out-dir ack-file [declared-source-sha256]
//
// The worker writes out-dir/chunk.pdf, out-dir/manifest.json and finally
// out-dir/ready (content "ready"), then waits for the host to create ack-file before it
// exits 0. The derivative is an operational artifact: the manifest maps every
// local page to the physical page of the original evidence. It never logs PDF
// content; errors contain only paths, numbers and library error strings.
//
// Do not use for evidentiary decisions; see experiments/pdf-chunker/reports/RESULTS.md.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

const (
	schema        = "viii.pdf-chunk.v1"
	maxPages      = 30000
	ackTimeout    = 120 * time.Second
	ackPoll       = 100 * time.Millisecond
	chunkName     = "chunk.pdf"
	manifestName  = "manifest.json"
	readyName     = "ready"
	pdfcpuModPath = "github.com/pdfcpu/pdfcpu"
)

var shaPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Several pdfcpu helpers (e.g. api.PageCountFile) ignore the caller's
// configuration and call model.NewDefaultConfiguration, which tries to create
// a config directory under os.UserConfigDir/os.TempDir and panics when that is
// impossible, as in a WASI sandbox. Disable it process-wide.
func init() { model.ConfigPath = "disable" }

type request struct {
	input, outDir, ack, declaredSHA string
	first, last                     int
}

type pageMap struct {
	Local    int `json:"local"`
	Physical int `json:"physical"`
}

type manifest struct {
	Schema                string    `json:"schema"`
	Tool                  string    `json:"tool"`
	ToolVersion           string    `json:"tool_version"`
	GoVersion             string    `json:"go_version"`
	GOOS                  string    `json:"goos"`
	GOARCH                string    `json:"goarch"`
	SourceBytes           int64     `json:"source_bytes"`
	SourceSHA256Declared  string    `json:"source_sha256_declared,omitempty"`
	PhysicalFirst         int       `json:"physical_first"`
	PhysicalLast          int       `json:"physical_last"`
	Pages                 []pageMap `json:"pages"`
	ChunkSHA256           string    `json:"chunk_sha256"`
	ChunkBytes            int64     `json:"chunk_bytes"`
	ChunkPageCount        int       `json:"chunk_page_count"`
	ElapsedMillis         int64     `json:"elapsed_ms"`
	GoRuntimeSysBytes     uint64    `json:"go_runtime_sys_bytes"`
	GoRuntimeTotalAlloc   uint64    `json:"go_runtime_total_alloc_bytes"`
	GoRuntimeHeapSysBytes uint64    `json:"go_runtime_heap_sys_bytes"`
}

func parseArgs(args []string) (request, error) {
	var r request
	if len(args) != 5 && len(args) != 6 {
		return r, errors.New("usage: chunker original.pdf first last out-dir ack-file [source-sha256]")
	}
	r.input, r.outDir, r.ack = args[0], args[3], args[4]
	var err error
	if r.first, err = strconv.Atoi(args[1]); err != nil || r.first < 1 {
		return r, errors.New("invalid first page")
	}
	if r.last, err = strconv.Atoi(args[2]); err != nil || r.last < r.first || r.last > maxPages {
		return r, errors.New("invalid last page")
	}
	if len(args) == 6 {
		if !shaPattern.MatchString(args[5]) {
			return r, errors.New("declared source sha256 must be 64 lowercase hex digits")
		}
		r.declaredSHA = args[5]
	}
	return r, nil
}

func pageMapping(first, last int) []pageMap {
	pages := make([]pageMap, 0, last-first+1)
	for p := first; p <= last; p++ {
		pages = append(pages, pageMap{Local: p - first + 1, Physical: p})
	}
	return pages
}

func digest(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), n, nil
}

func pdfcpuVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == pdfcpuModPath {
				return dep.Version
			}
		}
	}
	return "unknown"
}

// Bacalhau v1.9.5's WASI result mount (/out) on Windows was observed to
// create an empty file on any stat/open of a missing path, to ignore O_EXCL,
// and to not implement rename or remove (see RESULTS.md). The worker therefore
// never stats inside out-dir, checks pre-existing outputs with ReadDir, writes
// final names directly and writes the ready marker last. The host must accept
// only ready == "ready" plus a manifest whose chunk hash matches.
func existingOutputs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var found []string
	for _, e := range entries {
		switch e.Name() {
		case chunkName, manifestName, readyName:
			found = append(found, e.Name())
		}
	}
	return found, nil
}

func trimTo(ctx context.Context, input, output string, first, last int, conf *model.Configuration) (int, error) {
	in, err := os.Open(input)
	if err != nil {
		return 0, fmt.Errorf("open original: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, fmt.Errorf("create chunk: %w", err)
	}
	selection := []string{fmt.Sprintf("%d-%d", first, last)}
	if err := api.Trim(ctx, in, out, selection, conf); err != nil {
		out.Close()
		return 0, fmt.Errorf("pdfcpu trim: %w", err)
	}
	if err := out.Close(); err != nil {
		return 0, fmt.Errorf("close chunk: %w", err)
	}
	check, err := os.Open(output)
	if err != nil {
		return 0, fmt.Errorf("reopen chunk: %w", err)
	}
	defer check.Close()
	count, err := api.PageCount(ctx, check, conf)
	if err != nil {
		return 0, fmt.Errorf("count derivative pages: %w", err)
	}
	return count, nil
}

// chunk extracts [first,last] and verifies the derivative has exactly that many pages.
func chunk(ctx context.Context, r request) (*manifest, error) {
	started := time.Now()
	info, err := os.Stat(r.input)
	if err != nil {
		return nil, fmt.Errorf("stat original: %w", err)
	}
	if err := os.MkdirAll(r.outDir, 0o700); err != nil {
		return nil, fmt.Errorf("output directory: %w", err)
	}
	if found, err := existingOutputs(r.outDir); err != nil {
		return nil, fmt.Errorf("list output directory: %w", err)
	} else if len(found) > 0 {
		return nil, fmt.Errorf("refusing to overwrite existing %v", found)
	}
	// Stateless: the default configuration tries to create a config dir under
	// os.UserConfigDir/os.TempDir, which fails in a WASI sandbox.
	conf := model.NewStatelessConfiguration()
	output := filepath.Join(r.outDir, chunkName)
	committed := false
	defer func() { // best effort (remove is unavailable in Bacalhau /out); also runs on panic
		if !committed {
			_ = os.Remove(output)
		}
	}()
	count, err := trimTo(ctx, r.input, output, r.first, r.last, conf)
	if err != nil {
		return nil, err
	}
	if want := r.last - r.first + 1; count != want {
		return nil, fmt.Errorf("derivative has %d pages, expected %d (range beyond document?)", count, want)
	}
	committed = true
	hash, size, err := digest(output)
	if err != nil {
		return nil, err
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return &manifest{
		Schema: schema, Tool: "pdfcpu", ToolVersion: pdfcpuVersion(),
		GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		SourceBytes: info.Size(), SourceSHA256Declared: r.declaredSHA,
		PhysicalFirst: r.first, PhysicalLast: r.last, Pages: pageMapping(r.first, r.last),
		ChunkSHA256: hash, ChunkBytes: size, ChunkPageCount: count,
		ElapsedMillis:     time.Since(started).Milliseconds(),
		GoRuntimeSysBytes: ms.Sys, GoRuntimeTotalAlloc: ms.TotalAlloc, GoRuntimeHeapSysBytes: ms.HeapSys,
	}, nil
}

func waitAck(path string, timeout, poll time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("host acknowledgement timed out")
		}
		time.Sleep(poll)
	}
}

func run(args []string) error {
	r, err := parseArgs(args)
	if err != nil {
		return err
	}
	m, err := chunk(context.Background(), r)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.outDir, manifestName), data, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.outDir, readyName), []byte("ready"), 0o600); err != nil {
		return err
	}
	return waitAck(r.ack, ackTimeout, ackPoll)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "chunker:", err)
		os.Exit(1)
	}
}
