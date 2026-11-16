//go:build !wasip1

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/spikework"
	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
)

type workload struct {
	Name        string `json:"name"`
	TargetMS    int    `json:"target_ms"`
	InputBytes  int    `json:"input_bytes"`
	Rounds      uint64 `json:"rounds"`
	Concurrency int    `json:"concurrency"`
	Count       int    `json:"count"`
}
type path struct {
	name, node, stage string
}
type sample struct {
	Workload        workload         `json:"workload"`
	Path            string           `json:"path"`
	Phase           string           `json:"phase"`
	Index           int              `json:"index"`
	Started         time.Time        `json:"started"`
	ElapsedNS       int64            `json:"elapsed_ns"`
	PrepareNS       int64            `json:"prepare_ns"`
	DecodeNS        int64            `json:"decode_ns"`
	SubprocessCPUNS int64            `json:"subprocess_cpu_ns,omitempty"`
	ExpectedSHA256  string           `json:"expected_input_sha256"`
	Result          spikework.Output `json:"result"`
	Bacalhau        *wasijob.Trace   `json:"bacalhau,omitempty"`
	Error           string           `json:"error,omitempty"`
	TimingExcluded  bool             `json:"timing_excluded,omitempty"`
	TimedOut        bool             `json:"timed_out,omitempty"`
}

type distribution struct {
	MinMS    float64 `json:"min_ms"`
	MedianMS float64 `json:"median_ms"`
	P95MS    float64 `json:"p95_ms"`
	MaxMS    float64 `json:"max_ms"`
	MADMS    float64 `json:"mad_ms"`
}

func stats(ns []int64) distribution {
	if len(ns) == 0 {
		return distribution{}
	}
	ms := make([]float64, len(ns))
	for i, n := range ns {
		ms[i] = float64(n) / 1e6
	}
	sort.Float64s(ms)
	middle := func(values []float64) float64 {
		n := len(values)
		return (values[(n-1)/2] + values[n/2]) / 2
	}
	median := middle(ms)
	deviations := make([]float64, len(ms))
	for i, x := range ms {
		deviations[i] = math.Abs(x - median)
	}
	sort.Float64s(deviations)
	return distribution{ms[0], median, ms[int(math.Ceil(.95*float64(len(ms))))-1], ms[len(ms)-1], middle(deviations)}
}

func calibrate(input []byte) float64 {
	const rounds = 50_000_000
	durations := make([]int64, 3)
	for i := range durations {
		result, _ := spikework.Run(context.Background(), input, rounds)
		durations[i] = result.ComputeNS
	}
	return rounds / stats(durations).MedianMS
}

func runSample(executable, root string, client wasijob.Client, p path, w workload, phase string, index int, input []byte, expected spikework.Output) (s sample) {
	timeout := 45 * time.Second
	if w.TargetMS >= 10000 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	started := time.Now()
	s = sample{Workload: w, Path: p.name, Phase: phase, Index: index, Started: started.UTC(), ExpectedSHA256: expected.InputSHA256}
	var data []byte
	var err error
	filename := fmt.Sprintf("input-%d.bin", w.InputBytes)
	switch p.name {
	case "in-process":
		s.Result, err = spikework.Run(ctx, input, w.Rounds)
	case "subprocess":
		cmd := exec.CommandContext(ctx, executable, filepath.Join(root, filename), strconv.FormatUint(w.Rounds, 10))
		s.PrepareNS = time.Since(started).Nanoseconds()
		data, err = cmd.Output()
		if cmd.ProcessState != nil {
			s.SubprocessCPUNS = (cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()).Nanoseconds()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			err = fmt.Errorf("%w: %s", err, exit.Stderr)
		}
	default:
		spec := wasijob.Spec(fmt.Sprintf("viii-bench-%d-%d", started.UnixNano(), index), p.node,
			wasijob.StageURL(p.stage, "bench-worker.wasm"), wasijob.Input{Kind: "urldownload", URL: wasijob.StageURL(p.stage, filename), Target: "/inputs"},
			[]string{"/inputs/" + filename, strconv.FormatUint(w.Rounds, 10)}, int(timeout.Seconds())-5)
		s.PrepareNS = time.Since(started).Nanoseconds()
		var result wasijob.Trace
		data, result, err = client.Run(ctx, p.node, spec)
		s.Bacalhau = &result
	}
	if err == nil && p.name != "in-process" {
		decode := time.Now()
		err = json.Unmarshal(data, &s.Result)
		s.DecodeNS = time.Since(decode).Nanoseconds()
	}
	if err == nil && (s.Result.InputSHA256 != expected.InputSHA256 || s.Result.Checksum != expected.Checksum) {
		err = fmt.Errorf("output diverged from deterministic in-process reference")
	}
	s.ElapsedNS = time.Since(started).Nanoseconds()
	s.TimingExcluded = err == nil && (s.ElapsedNS <= 0 || s.Result.ComputeNS <= 0)
	if err != nil {
		s.Error = err.Error()
		s.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	}
	return s
}

func measure(args []string) error {
	flags := flag.NewFlagSet("measure", flag.ContinueOnError)
	short := flags.Bool("short", false, "smoke run of the core matrix")
	scaledOnly := flags.Bool("scaled-only", false, "run only the duration sweep calibrated against WASI")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *short && *scaledOnly {
		return errors.New("-short and -scaled-only are mutually exclusive")
	}
	root := os.Getenv("VIII_SPIKE_RESULTS")
	if root == "" {
		return errors.New("set VIII_SPIKE_RESULTS via the experiment script")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	client := wasijob.Client{Base: os.Getenv("VIII_SPIKE_API")}
	paths := []path{{name: "in-process"}, {name: "subprocess"}}
	if *scaledOnly && client.Base == "" {
		return errors.New("WASI calibration requires the experiment cluster")
	}
	if client.Base != "" {
		paths = append(paths, path{"wasi-a", "viii-a", os.Getenv("VIII_SPIKE_STAGE_A")})
		if stage := os.Getenv("VIII_SPIKE_STAGE_B"); stage != "" {
			paths = append(paths, path{"wasi-b", "viii-b", stage})
		}
	}
	sizes := []int{10 * 1024, 100 * 1024, 1024 * 1024}
	inputs := make(map[int][]byte)
	for _, size := range sizes {
		b := make([]byte, size)
		for i := range b {
			b[i] = byte((i*31 + i/257) % 251)
		}
		inputs[size] = b
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("input-%d.bin", size)), b, 0600); err != nil {
			return err
		}
	}
	roundsPerMS := calibrate(inputs[sizes[0]])
	var matrix []workload
	if !*scaledOnly {
		count := 10
		if *short {
			count = 4
		}
		for _, target := range []int{10, 100, 1000} {
			matrix = append(matrix, workload{"cpu", target, sizes[0], uint64(roundsPerMS * float64(target)), 1, count})
		}
		for _, size := range sizes[1:] {
			matrix = append(matrix, workload{"input", 10, size, matrix[0].Rounds, 1, count})
		}
		for _, concurrency := range []int{2, 4} {
			matrix = append(matrix, workload{"concurrency", 100, sizes[0], matrix[1].Rounds, concurrency, count})
		}
	}
	raw, err := os.Create(filepath.Join(root, "latency.jsonl"))
	if err != nil {
		return err
	}
	defer raw.Close()
	encoder := json.NewEncoder(raw)
	binaryHashes := map[string]string{}
	for _, file := range []string{executable, filepath.Join(root, "bench-worker.wasm")} {
		if b, err := os.ReadFile(file); err == nil {
			binaryHashes[filepath.Base(file)] = wasijob.Digest(b)
		}
	}
	if err := encoder.Encode(map[string]any{
		"event": "metadata", "topology": os.Getenv("VIII_SPIKE_TOPOLOGY"), "go": runtime.Version(), "os": runtime.GOOS,
		"arch": runtime.GOARCH, "cpus": runtime.NumCPU(), "rounds_per_ms": roundsPerMS, "started": time.Now().UTC(),
		"short": *short, "scaled_only": *scaledOnly, "binary_sha256": binaryHashes, "matrix": matrix,
	}); err != nil {
		return err
	}
	// Record a cluster-ready idle interval before the first worker use.
	time.Sleep(10 * time.Second)
	failed := 0
	seen := map[string]bool{}
	runMatrix := func(matrix []workload) error {
		for _, w := range matrix {
			input := inputs[w.InputBytes]
			var reference spikework.Output
			for _, p := range paths {
				phase := "warmup"
				if !seen[p.name] {
					phase = "first-use"
					seen[p.name] = true
				}
				if p.name == "in-process" {
					// The first in-process run is both the reference and this cell's warm-up.
					started := time.Now()
					reference, err = spikework.Run(context.Background(), input, w.Rounds)
					if err != nil {
						return err
					}
					if err := encoder.Encode(sample{Workload: w, Path: p.name, Phase: phase, Started: started.UTC(), ElapsedNS: time.Since(started).Nanoseconds(), Result: reference, ExpectedSHA256: reference.InputSHA256}); err != nil {
						return err
					}
				} else {
					warmup := runSample(executable, root, client, p, w, phase, 0, input, reference)
					if err := encoder.Encode(warmup); err != nil {
						return err
					}
					if warmup.Error != "" {
						failed++
						if phase == "first-use" {
							return fmt.Errorf("%s first benchmark job: %s", p.name, warmup.Error)
						}
					}
				}
				batch := time.Now()
				results := make(chan sample, w.Count)
				var group sync.WaitGroup
				for worker := 0; worker < w.Concurrency; worker++ {
					group.Go(func() {
						for i := worker; i < w.Count; i += w.Concurrency {
							results <- runSample(executable, root, client, p, w, "sample", i+1, input, reference)
						}
					})
				}
				go func() { group.Wait(); close(results) }()
				var elapsed, compute, residual []int64
				completed, excluded := 0, 0
				for s := range results {
					if err := encoder.Encode(s); err != nil {
						return err
					}
					if s.Error != "" {
						failed++
						continue
					}
					completed++
					if s.TimingExcluded {
						excluded++
						continue
					}
					elapsed = append(elapsed, s.ElapsedNS)
					compute = append(compute, s.Result.ComputeNS)
					residual = append(residual, s.ElapsedNS-s.Result.ComputeNS)
				}
				wall := time.Since(batch).Seconds()
				summary := map[string]any{"event": "summary", "workload": w, "path": p.name, "successful": completed, "timing_excluded": excluded,
					"failed": w.Count - completed, "batch_seconds": wall, "jobs_per_second": float64(completed) / wall,
					"elapsed": stats(elapsed), "compute": stats(compute), "elapsed_minus_compute": stats(residual)}
				if err := encoder.Encode(summary); err != nil {
					return err
				}
				fmt.Printf("%-11s %-11s %5d ms %7d bytes c=%d: %d/%d, median %.1f ms\n",
					p.name, w.Name, w.TargetMS, w.InputBytes, w.Concurrency, completed, w.Count, stats(elapsed).MedianMS)
			}
		}
		return nil
	}
	if err := runMatrix(matrix); err != nil {
		return err
	}
	if !*short {
		// Calibrate WASI with one common round count. The derived workloads
		// still use identical rounds and byte content on every path.
		input := inputs[sizes[0]]
		pilot := workload{"wasi-calibration", 100, sizes[0], uint64(roundsPerMS * 100), 1, 1}
		baseline, err := spikework.Run(context.Background(), input, pilot.Rounds)
		if err != nil {
			return err
		}
		var rate float64
		for _, p := range paths[2:] {
			phase := "calibration"
			if !seen[p.name] {
				phase = "first-use-calibration"
				seen[p.name] = true
			}
			result := runSample(executable, root, client, p, pilot, phase, 1, input, baseline)
			if err := encoder.Encode(result); err != nil {
				return err
			}
			if result.Error != "" || result.Result.ComputeNS <= 0 {
				return fmt.Errorf("WASI calibration on %s: %s", p.name, result.Error)
			}
			rate += float64(pilot.Rounds) * 1e6 / float64(result.Result.ComputeNS)
		}
		if len(paths) == 2 {
			rate = roundsPerMS
		} else {
			rate /= float64(len(paths) - 2)
		}
		var scaled []workload
		for _, target := range []int{10, 100, 1000, 10000, 60000} {
			count := 10
			if target >= 10000 {
				count = 2
			}
			scaled = append(scaled, workload{"cpu-scaled", target, sizes[0], uint64(rate * float64(target)), 1, count})
		}
		if err := encoder.Encode(map[string]any{"event": "scaled_plan", "rounds_per_ms": rate, "matrix": scaled}); err != nil {
			return err
		}
		if err := runMatrix(scaled); err != nil {
			return err
		}
	}
	if err := encoder.Encode(map[string]any{"event": "finished", "at": time.Now().UTC(), "failures": failed}); err != nil {
		return err
	}
	if failed != 0 {
		return fmt.Errorf("%d samples or warm-ups failed; see latency.jsonl", failed)
	}
	return nil
}
