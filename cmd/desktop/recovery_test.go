package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
)

func testRecovery(t *testing.T, ctx context.Context, service *workbench, p *wasijob.Placement, input []byte, jobs func() []string, write func(string, any)) {
	t.Helper()
	p.DelaySeconds = 35
	observationWindow := 95 * time.Second
	var controlJob string
	if os.Getenv("VIII_SPIKE_FAILOVER_PROBE") == "1" {
		p.DelaySeconds = 100
		observationWindow = 70 * time.Second
		controlJob = startFailoverProbe(t, ctx, p, write)
	}
	url := "/api/process?mode=bacalhau&node=viii-b"
	a := callAttempt(t, service, "POST", url, input, 202)
	if a["status"] != "accepted" || a["job_id"] == nil {
		t.Fatalf("submission did not return a persisted job ID: %v", a)
	}
	write("accepted_before_restart", observation(a))
	service = &workbench{root: service.root, placement: p}
	before := jobs()
	duplicate := callAttempt(t, service, "POST", url, input, 202)
	if duplicate["attempt"] != a["attempt"] || !reflect.DeepEqual(before, jobs()) {
		t.Fatal("reconstructed service duplicated the active job")
	}
	write("restarted_no_duplicate", map[string]any{"attempt": duplicate["attempt"], "job_ids": before})

	marker := fmt.Sprintf("VIII_NORMALIZED sha256=%s messages=32", digest(input))
	var previous string
	poll := func(service *workbench, timeout time.Duration, event string, done func(map[string]any) bool) bool {
		t.Helper()
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) && ctx.Err() == nil {
			a = callAttempt(t, service, "GET", attemptPath(a), nil, 200, 202)
			current, err := json.Marshal([]any{a["status"], a["job_state"], a["executions"], a["error"], a["diagnostic"]})
			if err != nil {
				t.Fatal(err)
			}
			if string(current) != previous {
				write(event, observation(a))
				previous = string(current)
			}
			if done(a) {
				return true
			}
			time.Sleep(250 * time.Millisecond)
		}
		return false
	}
	if !poll(service, 25*time.Second, "before_kill", func(a map[string]any) bool {
		id, _ := a["execution_id"].(string)
		if a["finished"] != nil {
			t.Fatalf("job finished before injection: %v", observation(a))
		}
		if a["node_id"] != "viii-b" || id == "" {
			return false
		}
		line := workerMarker(t, id, marker)
		if line == nil {
			return false
		}
		write("worker_normalized", map[string]any{"job_id": a["job_id"], "execution_id": id, "node_id": a["node_id"], "log": line})
		return true
	}) {
		t.Fatal("worker normalization was not observed; node was left running")
	}
	killNode(t)
	write("node_killed", observation(a))

	service = &workbench{root: service.root, placement: p}
	terminal := poll(service, observationWindow, "after_kill", func(a map[string]any) bool {
		return a["finished"] != nil
	})
	if !terminal {
		cancel := callAttempt(t, service, http.MethodDelete, attemptPath(a), nil, 200, 202)
		write("cancel_interrupted", observation(cancel))
		terminal = poll(service, 10*time.Second, "after_cancel", func(a map[string]any) bool { return a["finished"] != nil })
	} else {
		write("cancel_interrupted_terminal", observation(callAttempt(t, service, http.MethodDelete, attemptPath(a), nil, 200)))
	}
	if !terminal || (a["status"] != "failed" && a["status"] != "stopped") {
		t.Fatalf("interrupted job was not reconciled: %v", observation(a))
	}
	interrupted := a
	if !reflect.DeepEqual(before, jobs()) {
		t.Fatal("reconciliation submitted more jobs")
	}
	assertAuthorizedExecutions(t, a, "viii-b")
	if controlJob != "" {
		observeFailoverProbe(t, ctx, p.Client, controlJob, write)
	}

	p.DelaySeconds = 30
	cancelProbe := callAttempt(t, service, http.MethodPost, "/api/process?mode=bacalhau&node=viii-a", input, 202)
	cancelProbe = callAttempt(t, service, http.MethodDelete, attemptPath(cancelProbe), nil, 200, 202)
	write("cancel_accepted_job", observation(cancelProbe))
	a = cancelProbe
	if !poll(service, 10*time.Second, "cancel_probe_state", func(a map[string]any) bool { return a["finished"] != nil }) || a["status"] != "stopped" {
		t.Fatalf("accepted-job cancellation did not stop the job: %v", observation(a))
	}

	p.DelaySeconds = 0
	a = callAttempt(t, service, "POST", "/api/process?mode=bacalhau&node=viii-a&retry_of="+interrupted["attempt"].(string), input, 202)
	if !poll(service, 25*time.Second, "manual_retry", func(a map[string]any) bool { return a["finished"] != nil }) ||
		a["status"] != "completed" || a["retry_of"] != interrupted["attempt"] ||
		a["request_id"] != interrupted["request_id"] || a["attempt"] == interrupted["attempt"] ||
		a["job_id"] == interrupted["job_id"] || a["node_id"] != "viii-a" || a["worker_sha256"] != interrupted["worker_sha256"] {
		t.Fatalf("retry failed provenance checks: %v", observation(a))
	}
	assertSynthetic(t, a)
	dirs, err := os.ReadDir(service.root)
	if err != nil || len(dirs) != 1 || dirs[0].Name() != digest(input) ||
		!bytes.Equal(input, readSpikeFile(t, filepath.Join(service.root, digest(input), "original.xml"))) {
		t.Fatalf("evidence identity or bytes changed: %v, %v", dirs, err)
	}
	staged, err := os.ReadDir(p.StagingDir)
	if err != nil || len(staged) != 0 {
		t.Fatalf("staged inputs remain: %v, %v", staged, err)
	}
	state := callAttempt(t, service, "GET", attemptPath(interrupted), nil, 200)
	if !reflect.DeepEqual(state, interrupted) {
		t.Fatal("interrupted attempt changed during manual retry")
	}
	write("evidence_preserved", map[string]any{"sha256": digest(input), "bytes": len(input), "message_count": 32, "evidence_directories": len(dirs), "staged_files_remaining": len(staged)})
}

func observation(a map[string]any) map[string]any {
	out := make(map[string]any)
	for key, value := range a {
		if key != "result" && key != "original_xml" && key != "staging_dir" {
			out[key] = value
		}
	}
	return out
}

func assertAuthorizedExecutions(t *testing.T, a map[string]any, node string) {
	t.Helper()
	executions, _ := a["executions"].([]any)
	if len(executions) == 0 {
		t.Fatal("no persisted execution provenance")
	}
	for _, value := range executions {
		if x := value.(map[string]any); x["NodeID"] != node {
			t.Fatalf("unexpected placement: %v", x)
		}
	}
}

func workerMarker(t *testing.T, id, marker string) map[string]any {
	t.Helper()
	var data []byte
	if container := os.Getenv("VIII_SPIKE_B_CONTAINER"); container != "" {
		if strings.Trim(id, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			t.Fatal("unexpected execution ID format")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, _ := nodeDocker(ctx, "exec", container, "sh", "-c", "cat /tmp/"+id+"_log.json*").Output()
		data = out
	} else {
		files, err := filepath.Glob(filepath.Join(os.Getenv("VIII_SPIKE_B_TEMP"), id+"_log.json*"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			raw, _ := os.ReadFile(file)
			data = append(data, raw...)
		}
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event struct {
			Stream int    `json:"s"`
			Data   []byte `json:"d"`
			At     int64  `json:"t"`
		}
		if json.Unmarshal(line, &event) == nil && event.Stream == 2 && strings.TrimSpace(string(event.Data)) == marker {
			return map[string]any{"stream": "stderr", "message": string(event.Data), "unix_seconds": event.At, "raw": string(line)}
		}
	}
	return nil
}

func killNode(t *testing.T) {
	t.Helper()
	switch {
	case os.Getenv("VIII_SPIKE_B_CONTAINER") != "":
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if output, err := nodeDocker(ctx, "kill", os.Getenv("VIII_SPIKE_B_CONTAINER")).CombinedOutput(); err != nil {
			t.Fatalf("docker kill: %v: %s", err, output)
		}
	default:
		pid, err := strconv.Atoi(os.Getenv("VIII_SPIKE_B_PID"))
		if err != nil || pid <= 0 {
			t.Fatal("missing isolated node B PID")
		}
		process, err := os.FindProcess(pid)
		if err != nil {
			t.Fatal(err)
		}
		if err = process.Kill(); err != nil {
			t.Fatal(err)
		}
	}
}

func nodeDocker(ctx context.Context, args ...string) *exec.Cmd {
	if host := os.Getenv("VIII_SPIKE_B_SSH"); host != "" {
		if args[0] == "exec" {
			args[len(args)-1] = "'" + args[len(args)-1] + "'"
		}
		return exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes",
			"-o", "ConnectTimeout=8", host, "docker "+strings.Join(args, " "))
	}
	return exec.CommandContext(ctx, "docker", args...)
}

const failoverInput = `<project xmlns="http://pa.cellebrite.com/report/2.0"><decodedData/></project>`

func startFailoverProbe(t *testing.T, ctx context.Context, p *wasijob.Placement, write func(string, any)) string {
	t.Helper()
	client := p.Client
	if p.Modules["viii-a"] != p.Modules["viii-b"] {
		t.Fatal("failover probe needs a worker URL reachable from both nodes")
	}
	setA := func(action string) {
		t.Helper()
		var response struct {
			Success bool
			Error   string
		}
		if err := client.Request(ctx, http.MethodPut, "/nodes/viii-a", map[string]string{"Action": action}, &response); err != nil || !response.Success {
			t.Fatalf("A membership action %s: %+v %v", action, response, err)
		}
		write("control_membership_a", action)
	}

	// Keep only B eligible for initial placement, then bring A back before
	// killing B. The control job contains an empty report and no evidence.
	setA("reject")
	input := []byte(failoverInput)
	spec := wasijob.Spec("viii-scheduler-control", "", p.Modules["viii-b"],
		wasijob.Input{Kind: "inline", URL: wasijob.InlineXML(input), Target: "/input.xml"},
		[]string{"/input.xml", "-delay-seconds", "100"}, 180)
	spec["Constraints"] = []any{map[string]any{"Key": "name", "Operator": "in", "Values": []string{"viii-a", "viii-b"}}}
	job, err := client.Submit(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	write("control_accepted", map[string]any{"job_id": job, "allowed_nodes": []string{"viii-a", "viii-b"}, "input_sha256": digest(input)})
	started := false
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		d, err := client.Describe(ctx, job)
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range d.Executions.Items {
			if x.NodeID != "viii-b" {
				t.Fatalf("unexpected initial control placement: %+v", x)
			}
			line := workerMarker(t, x.ID, fmt.Sprintf("VIII_NORMALIZED sha256=%s messages=0", digest(input)))
			if line != nil {
				write("control_running_b", map[string]any{"job_id": job, "execution": x, "worker_log": line})
				started = true
				break
			}
		}
		if started {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !started {
		t.Fatal("control did not start on B")
	}

	setA("approve")
	deadline = time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		var node struct {
			Node struct {
				Membership      string
				ConnectionState struct{ Status string }
			}
		}
		if err := client.Request(ctx, http.MethodGet, "/nodes/viii-a", nil, &node); err != nil {
			t.Fatal(err)
		}
		if node.Node.Membership == "APPROVED" && node.Node.ConnectionState.Status == "CONNECTED" {
			write("control_a_eligible", node.Node)
			return job
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("A did not rejoin the scheduler before fault injection")
	return ""
}

func observeFailoverProbe(t *testing.T, ctx context.Context, client wasijob.Client, job string, write func(string, any)) {
	t.Helper()
	var last []byte
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		d, err := client.Describe(ctx, job)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(snapshot, last) {
			write("control_after_kill", d)
			last = snapshot
		}
		for _, x := range d.Executions.Items {
			// Bacalhau exposes BidAccepted (5) as expected to run, while the
			// actual worker start is proved by a marker only on B here.
			if x.NodeID == "viii-a" && (x.ComputeState.StateType == 5 || x.ComputeState.StateType == wasijob.ExecutionRunning) {
				write("control_rescheduled_a", map[string]any{"job_id": job, "execution": x})
				if err = client.Stop(ctx, job); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		switch d.Job.State.StateType {
		case "Failed", "Stopped", "Completed":
			write("control_terminal", d.Job.State)
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	write("control_observation_timeout", map[string]any{"job_id": job})
	if err := client.Stop(ctx, job); err != nil {
		t.Fatal(err)
	}
}
