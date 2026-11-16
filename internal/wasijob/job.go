// Package wasijob contains the synthetic spike's shared Bacalhau job contract.
package wasijob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const api = "/api/v1/orchestrator"

func Digest(b []byte) string {
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

func InlineXML(b []byte) string {
	return "data:text/xml;base64," + base64.StdEncoding.EncodeToString(b)
}

type Input struct{ Kind, URL, Target string }

type Options struct {
	TotalTimeout int
	CPU          string
}

// Spec preserves the existing one-task WASI worker interface. In v1.9.5 the
// built-in name label is the persisted NodeID.
func Spec(name, node, module string, input Input, args []string, timeout int) map[string]any {
	return WASISpec(name, node, module, input, args, Options{TotalTimeout: timeout})
}

func WASISpec(name, node, module string, input Input, args []string, options Options) map[string]any {
	engine := map[string]any{"Type": "wasm", "Params": map[string]any{"EntryModule": "/worker.wasm", "Entrypoint": "_start", "Parameters": args}}
	return jobSpec(name, node, engine, []Input{{"urldownload", module, "/worker.wasm"}, input}, options)
}

// DockerSpec runs the same benchmark worker via Bacalhau's Docker executor.
func DockerSpec(name, node, image string, input Input, args []string, options Options) map[string]any {
	engine := map[string]any{"Type": "docker", "Params": map[string]any{"Image": image, "Entrypoint": []string{"/bench-worker"}, "Parameters": args}}
	return jobSpec(name, node, engine, []Input{input}, options)
}

func jobSpec(name, node string, engine map[string]any, inputs []Input, options Options) map[string]any {
	sources := make([]any, 0, len(inputs))
	for _, input := range inputs {
		sources = append(sources, map[string]any{"Source": map[string]any{"Type": input.Kind, "Params": map[string]string{"URL": input.URL}}, "Target": input.Target})
	}
	task := map[string]any{
		"Name":         "main",
		"Engine":       engine,
		"InputSources": sources,
	}
	if options.TotalTimeout > 0 {
		task["Timeouts"] = map[string]int{"TotalTimeout": options.TotalTimeout}
	}
	if options.CPU != "" {
		task["Resources"] = map[string]string{"CPU": options.CPU, "Memory": "512mb"}
	}
	job := map[string]any{"Name": name, "Type": "batch", "Count": 1, "Tasks": []any{task}}
	if node != "" {
		job["Constraints"] = []any{map[string]any{"Key": "name", "Operator": "in", "Values": []string{node}}}
	}
	return job
}

type Client struct {
	Base string
}

func (c Client) Request(ctx context.Context, method, path string, body, out any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+api+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, b)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type Trace struct {
	JobID              string `json:"job_id,omitempty"`
	ExecutionID        string `json:"execution_id,omitempty"`
	NodeID             string `json:"node_id,omitempty"`
	State              string `json:"state,omitempty"`
	ObservedExecutions int    `json:"observed_executions"`
	SubmitNS           int64  `json:"submit_ns"`
	WaitAndFetchNS     int64  `json:"wait_and_fetch_ns"`
	FinalFetchNS       int64  `json:"final_fetch_ns"`
	CancelError        string `json:"cancel_error,omitempty"`
	Engine             string `json:"engine,omitempty"`
	CPU                string `json:"job_cpu,omitempty"`
	Memory             string `json:"job_memory,omitempty"`
	ExitCode           *int   `json:"exit_code,omitempty"`
	JobCreatedNS       int64  `json:"job_created_unix_ns,omitempty"`
	ExecutionCreatedNS int64  `json:"execution_created_unix_ns,omitempty"`
	ExecutionUpdatedNS int64  `json:"execution_updated_unix_ns,omitempty"`
}

func (c Client) Run(ctx context.Context, node string, spec map[string]any) (result []byte, trace Trace, err error) {
	var submission struct{ JobID string }
	start := time.Now()
	err = c.Request(ctx, http.MethodPut, "/jobs", map[string]any{"Job": spec}, &submission)
	trace.SubmitNS = time.Since(start).Nanoseconds()
	if err != nil {
		return nil, trace, err
	}
	trace.JobID = submission.JobID
	if trace.JobID == "" {
		return nil, trace, fmt.Errorf("missing job ID")
	}
	path := "/jobs/" + trace.JobID
	start = time.Now()
	defer func() {
		trace.WaitAndFetchNS = time.Since(start).Nanoseconds()
		if err != nil && trace.State != "Failed" && trace.State != "Completed" && trace.State != "Stopped" && trace.State != "Cancelled" && trace.State != "Canceled" {
			stop, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if stopErr := c.Request(stop, http.MethodDelete, path, nil, nil); stopErr != nil {
				trace.CancelError = stopErr.Error()
			}
		}
	}()
	poll := time.NewTicker(100 * time.Millisecond)
	defer poll.Stop()
	for {
		var description struct {
			Job struct {
				State struct{ StateType string }
				Tasks []struct {
					Engine    struct{ Type string }
					Resources struct{ CPU, Memory string }
				}
				CreateTime int64
			}
			Executions struct {
				Items []struct {
					ID, NodeID             string
					CreateTime, ModifyTime int64
					RunOutput              *struct {
						Stdout, Stderr, ErrorMsg string
						ExitCode                 int
						StdoutTruncated          bool
					}
				}
			}
		}
		fetch := time.Now()
		err = c.Request(ctx, http.MethodGet, path+"?include=executions", nil, &description)
		trace.FinalFetchNS = time.Since(fetch).Nanoseconds()
		if err != nil {
			return nil, trace, err
		}
		trace.State = description.Job.State.StateType
		trace.JobCreatedNS = description.Job.CreateTime
		trace.ObservedExecutions = len(description.Executions.Items)
		if len(description.Job.Tasks) == 1 {
			task := description.Job.Tasks[0]
			trace.Engine, trace.CPU, trace.Memory = task.Engine.Type, task.Resources.CPU, task.Resources.Memory
		}
		if trace.ObservedExecutions == 1 {
			x := description.Executions.Items[0]
			trace.ExecutionID, trace.NodeID = x.ID, x.NodeID
			trace.ExecutionCreatedNS, trace.ExecutionUpdatedNS = x.CreateTime, x.ModifyTime
			if x.RunOutput != nil {
				trace.ExitCode = &x.RunOutput.ExitCode
			}
		}
		switch trace.State {
		case "Completed":
			if trace.ObservedExecutions != 1 {
				return nil, trace, fmt.Errorf("expected one execution, got %d", trace.ObservedExecutions)
			}
			x := description.Executions.Items[0]
			trace.ExecutionID, trace.NodeID = x.ID, x.NodeID
			if x.ID == "" || (node != "" && x.NodeID != node) {
				return nil, trace, fmt.Errorf("invalid execution identity or placement: node %q, expected %q", x.NodeID, node)
			}
			if x.RunOutput == nil {
				return nil, trace, fmt.Errorf("missing worker output")
			}
			if x.RunOutput.ExitCode != 0 || x.RunOutput.StdoutTruncated || x.RunOutput.ErrorMsg != "" {
				return nil, trace, fmt.Errorf("worker exit %d: %s %s", x.RunOutput.ExitCode, x.RunOutput.ErrorMsg, x.RunOutput.Stderr)
			}
			return []byte(x.RunOutput.Stdout), trace, nil
		case "Failed", "Stopped", "Cancelled", "Canceled":
			return nil, trace, fmt.Errorf("job %s: %s", trace.JobID, trace.State)
		}
		select {
		case <-ctx.Done():
			return nil, trace, ctx.Err()
		case <-poll.C:
		}
	}
}

// Placement authorizes exactly one evidence digest and NodeID before encoding
// the inline input or calling the submission adapter.
type Placement struct {
	Allow        map[string]map[string]bool
	Modules      map[string]string
	WorkerSHA256 string
	Submit       func(context.Context, string, map[string]any) ([]byte, Trace, error)
}

func (p Placement) Run(ctx context.Context, attempt, node string, input []byte, record map[string]any) ([]byte, error) {
	id := Digest(input)
	record["requested_node_id"] = node
	record["authorization"] = "deny"
	if !p.Allow[id][node] || p.Modules[node] == "" {
		return nil, fmt.Errorf("evidence placement denied")
	}
	record["authorization"] = "allow"
	record["worker_sha256"] = p.WorkerSHA256
	start := time.Now()
	spec := Spec("viii-"+attempt, node, p.Modules[node], Input{Kind: "inline", URL: InlineXML(input), Target: "/input.xml"}, []string{"/input.xml"}, 35)
	record["prepare_ns"] = time.Since(start).Nanoseconds()
	result, trace, err := p.Submit(ctx, node, spec)
	record["trace"] = trace
	if trace.JobID != "" {
		record["job_id"] = trace.JobID
	}
	if trace.ExecutionID != "" {
		record["execution_id"] = trace.ExecutionID
	}
	if trace.NodeID != "" {
		record["node_id"] = trace.NodeID
	}
	return result, err
}

func StageURL(base, file string) string {
	return strings.TrimRight(base, "/") + "/" + file
}
