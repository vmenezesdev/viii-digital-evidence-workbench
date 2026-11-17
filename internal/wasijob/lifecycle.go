package wasijob

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type State[T string | int] struct {
	StateType T
	Message   string            `json:",omitempty"`
	Details   map[string]string `json:",omitempty"`
}

const ExecutionRunning = 6 // Bacalhau v1.9.5 models.ExecutionStateRunning.

type Execution struct {
	ID, NodeID, PreviousExecution, NextExecution string
	CreateTime, ModifyTime                       int64
	ComputeState, DesiredState                   State[int]
	RunOutput                                    *struct {
		Stdout, Stderr, ErrorMsg string
		ExitCode                 int
		StdoutTruncated          bool
	}
}

type Description struct {
	Job struct {
		ID, Name   string
		State      State[string]
		CreateTime int64
		Tasks      []struct {
			Engine    struct{ Type string }
			Resources struct{ CPU, Memory string }
		}
	}
	Executions struct{ Items []Execution }
}

func (c Client) Submit(ctx context.Context, spec map[string]any) (string, error) {
	var submission struct{ JobID string }
	err := c.Request(ctx, http.MethodPut, "/jobs", map[string]any{"Job": spec}, &submission)
	if err == nil && submission.JobID == "" {
		err = fmt.Errorf("missing job ID")
	}
	return submission.JobID, err
}

func (c Client) Describe(ctx context.Context, job string) (Description, error) {
	var d Description
	err := c.Request(ctx, http.MethodGet, "/jobs/"+url.PathEscape(job)+"?include=executions", nil, &d)
	return d, err
}

func (c Client) Stop(ctx context.Context, job string) error {
	return c.Request(ctx, http.MethodDelete, "/jobs/"+url.PathEscape(job), nil, nil)
}

func (d Description) Result(node string) ([]byte, error) {
	if len(d.Executions.Items) != 1 {
		return nil, fmt.Errorf("expected one execution, got %d", len(d.Executions.Items))
	}
	x := d.Executions.Items[0]
	if x.ID == "" || (node != "" && x.NodeID != node) {
		return nil, fmt.Errorf("invalid execution identity or placement: node %q, expected %q", x.NodeID, node)
	}
	if x.RunOutput == nil {
		return nil, fmt.Errorf("missing worker output")
	}
	if x.RunOutput.ExitCode != 0 || x.RunOutput.StdoutTruncated || x.RunOutput.ErrorMsg != "" {
		return nil, fmt.Errorf("worker exit %d (stdout truncated: %t): %s %s", x.RunOutput.ExitCode, x.RunOutput.StdoutTruncated, x.RunOutput.ErrorMsg, x.RunOutput.Stderr)
	}
	return []byte(x.RunOutput.Stdout), nil
}

type Placement struct {
	Allow        map[string]map[string]bool
	Modules      map[string]string
	WorkerSHA256 string
	Client       Client
	StagingURLs  map[string]string
	StagingDir   string
	DelaySeconds int
}

func (p *Placement) Authorize(id, node string) bool {
	return p != nil && node != "" && p.Allow[id][node] && p.Modules[node] != "" && p.WorkerSHA256 != "" && p.Client.Base != ""
}
