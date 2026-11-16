package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
)

func processAt(t *testing.T, service *workbench, mode, node string, fixture []byte) map[string]any {
	t.Helper()
	response := httptest.NewRecorder()
	service.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/process?mode="+mode+"&node="+node, bytes.NewReader(fixture)))
	var record map[string]any
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestPlacementPolicy(t *testing.T) {
	fixture := readSpikeFile(t, "../../docs/research/fixtures/ufdr/messages-minimal.xml")
	calls := 0
	p := wasijob.Placement{
		Allow:        map[string]map[string]bool{digest(fixture): {"viii-a": true}},
		Modules:      map[string]string{"viii-a": "http://localhost/worker.wasm", "viii-b": "http://localhost/worker.wasm"},
		WorkerSHA256: "unit-test",
		Submit: func(_ context.Context, node string, spec map[string]any) ([]byte, wasijob.Trace, error) {
			calls++
			b, err := json.Marshal(spec)
			if node != "viii-a" || err != nil || !bytes.Contains(b, []byte(wasijob.InlineXML(fixture))) || !bytes.Contains(b, []byte(`"Values":["viii-a"]`)) {
				t.Fatalf("unexpected authorized spec: %s (%v)", b, err)
			}
			return []byte("{}"), wasijob.Trace{JobID: "test-job", ExecutionID: "test-execution", NodeID: node}, nil
		},
	}
	service := &workbench{root: t.TempDir(), placement: &p}
	if got := processAt(t, service, "bacalhau", "viii-b", fixture); got["authorization"] != "deny" || got["job_id"] != nil || got["trace"] != nil || got["prepare_ns"] != nil || got["error"] == nil || calls != 0 {
		t.Fatalf("unexpected denial: %v, calls %d", got, calls)
	}
	other := bytes.Replace(fixture, []byte("library"), []byte("museum"), 1)
	if bytes.Equal(other, fixture) {
		t.Fatal("fixture no longer contains the test message")
	}
	if got := processAt(t, service, "bacalhau", "viii-a", other); got["authorization"] != "deny" || calls != 0 {
		t.Fatalf("unexpected access for another evidence ID: %v, calls %d", got, calls)
	}
	if got := processAt(t, service, "bacalhau", "viii-a", fixture); got["authorization"] != "allow" || got["error"] != nil || calls != 1 {
		t.Fatalf("unexpected allow: %v, calls %d", got, calls)
	}
}

func readSpikeFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDistributedSpike(t *testing.T) {
	root := os.Getenv("VIII_SPIKE_RESULTS")
	if root == "" {
		t.Skip("run scripts/spike-distributed.ps1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client := wasijob.Client{Base: os.Getenv("VIII_SPIKE_API")}
	report, err := os.Create(filepath.Join(root, "placement.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer report.Close()
	write := func(event string, data any) {
		t.Helper()
		if err := json.NewEncoder(report).Encode(map[string]any{"event": event, "at": time.Now().UTC(), "data": data}); err != nil {
			t.Fatal(err)
		}
	}
	membership := func(action string) {
		t.Helper()
		var response struct {
			Success bool
			Error   string
		}
		if err := client.Request(ctx, http.MethodPut, "/nodes/viii-b", map[string]string{"Action": action}, &response); err != nil || !response.Success {
			t.Fatalf("node %s: %+v %v", action, response, err)
		}
	}
	state := func(node, wantMembership, wantConnection string) {
		t.Helper()
		var response struct {
			Node struct {
				Info struct {
					NodeID          string
					Labels          map[string]string
					ComputeNodeInfo struct{ ExecutionEngines []string }
				}
				Membership      string
				ConnectionState struct{ Status string }
			}
		}
		if err := client.Request(ctx, http.MethodGet, "/nodes/"+node, nil, &response); err != nil {
			t.Fatal(err)
		}
		write("node_state", response.Node)
		if response.Node.Info.NodeID != node || !strings.EqualFold(response.Node.Membership, wantMembership) ||
			(wantConnection != "" && !strings.EqualFold(response.Node.ConnectionState.Status, wantConnection)) || !reflect.DeepEqual(response.Node.Info.ComputeNodeInfo.ExecutionEngines, []string{"wasm"}) {
			t.Fatalf("unexpected node state: %+v", response.Node)
		}
	}
	state("viii-a", "approved", "connected")
	state("viii-b", "pending", "")
	membership("reject")
	state("viii-b", "rejected", "")

	// This job contains no UFDR data and asks only for the rejected node.
	// If the scheduler leaves it pending, stop the probe after observing zero placements.
	probeCtx, stopProbe := context.WithTimeout(ctx, 5*time.Second)
	probe := wasijob.Spec("viii-admission-probe", "viii-b", wasijob.StageURL(os.Getenv("VIII_SPIKE_STAGE_B"), "worker.wasm"), wasijob.Input{Kind: "inline", URL: "data:text/plain;base64,cHJvYmU=", Target: "/probe.txt"}, []string{"/probe.txt"}, 8)
	_, trace, probeErr := client.Run(probeCtx, "viii-b", probe)
	stopProbe()
	write("admission_probe", map[string]any{"trace": trace, "error": fmt.Sprint(probeErr)})
	if probeErr == nil || trace.JobID == "" || trace.ObservedExecutions != 0 || trace.CancelError != "" {
		t.Fatalf("unexpected rejected-node probe: %+v %v", trace, probeErr)
	}
	membership("approve")
	// Rejected nodes can reconnect on their next heartbeat/handshake.
	deadline := time.Now().Add(45 * time.Second)
	for {
		var response struct {
			Node struct{ ConnectionState struct{ Status string } }
		}
		if err := client.Request(ctx, http.MethodGet, "/nodes/viii-b", nil, &response); err != nil {
			t.Fatal(err)
		}
		if strings.EqualFold(response.Node.ConnectionState.Status, "connected") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("approved node B did not reconnect")
		}
		time.Sleep(250 * time.Millisecond)
	}
	state("viii-b", "approved", "connected")

	fixture := readSpikeFile(t, "../../docs/research/fixtures/ufdr/messages-minimal.xml")
	id := digest(fixture)
	p := &wasijob.Placement{
		Allow: map[string]map[string]bool{id: {"viii-a": true}},
		Modules: map[string]string{
			"viii-a": wasijob.StageURL(os.Getenv("VIII_SPIKE_STAGE_A"), "worker.wasm"),
			"viii-b": wasijob.StageURL(os.Getenv("VIII_SPIKE_STAGE_B"), "worker.wasm"),
		},
		WorkerSHA256: digest(readSpikeFile(t, filepath.Join(root, "worker.wasm"))),
		Submit:       client.Run,
	}
	service := &workbench{root: filepath.Join(root, "evidence"), placement: p}
	native := processAt(t, service, "native", "", fixture)
	local := processAt(t, service, "bacalhau", "viii-a", fixture)
	save := func(event string, record map[string]any) {
		delete(record, "original_xml")
		write(event, record)
	}
	save("native", native)
	save("allow_a", local)
	if local["error"] != nil || local["node_id"] != "viii-a" || native["error"] != nil || !reflect.DeepEqual(native["result"], local["result"]) {
		t.Fatalf("local execution: %v", local)
	}
	jobs := func() []string {
		t.Helper()
		var response struct{ Items []struct{ ID string } }
		if err := client.Request(ctx, http.MethodGet, "/jobs", nil, &response); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, item := range response.Items {
			ids = append(ids, item.ID)
		}
		sort.Strings(ids)
		return ids
	}
	jobsBefore := jobs()
	stageLog := filepath.Join(root, "stage.stderr.log")
	stageBefore := readSpikeFile(t, stageLog)
	denied := processAt(t, service, "bacalhau", "viii-b", fixture)
	jobsAfter := jobs()
	stageAfter := readSpikeFile(t, stageLog)
	save("deny_b", denied)
	write("denial_observation", map[string]any{"jobs_before": jobsBefore, "jobs_after": jobsAfter, "stage_requests_before": bytes.Count(stageBefore, []byte("\n")), "stage_requests_after": bytes.Count(stageAfter, []byte("\n"))})
	if denied["authorization"] != "deny" || denied["job_id"] != nil || denied["trace"] != nil || denied["prepare_ns"] != nil || denied["error"] != "evidence placement denied" ||
		!reflect.DeepEqual(jobsBefore, jobsAfter) || !bytes.Equal(stageBefore, stageAfter) {
		t.Fatalf("denial disclosed evidence: %v, jobs %v -> %v", denied, jobsBefore, jobsAfter)
	}
	state("viii-b", "approved", "connected")
	p.Allow[id]["viii-b"] = true
	remote := processAt(t, service, "bacalhau", "viii-b", fixture)
	save("allow_b", remote)
	for node, record := range map[string]map[string]any{"viii-a": local, "viii-b": remote} {
		if record["error"] != nil || record["job_id"] == nil {
			t.Fatalf("job on %s: %v", node, record)
		}
		var submitted struct {
			Job struct {
				Constraints []struct {
					Key, Operator string
					Values        []string
				}
				Tasks []struct {
					Engine       struct{ Type string }
					InputSources []struct {
						Target string
						Source struct {
							Type   string
							Params map[string]string
						}
					}
				}
			}
		}
		if err := client.Request(ctx, http.MethodGet, "/jobs/"+record["job_id"].(string), nil, &submitted); err != nil {
			t.Fatal(err)
		}
		job := submitted.Job
		if len(job.Constraints) != 1 || job.Constraints[0].Key != "name" || job.Constraints[0].Operator != "in" ||
			!reflect.DeepEqual(job.Constraints[0].Values, []string{node}) || len(job.Tasks) != 1 ||
			job.Tasks[0].Engine.Type != "wasm" || len(job.Tasks[0].InputSources) != 2 {
			t.Fatalf("unexpected submitted shape for %s", node)
		}
		sources := job.Tasks[0].InputSources
		if sources[0].Source.Type != "urldownload" || sources[0].Source.Params["URL"] != p.Modules[node] ||
			sources[1].Source.Type != "inline" || sources[1].Target != "/input.xml" || sources[1].Source.Params["URL"] != wasijob.InlineXML(fixture) {
			t.Fatalf("unexpected submitted inputs for %s", node)
		}
		write("submitted_spec", map[string]any{"node_id": node, "job_id": record["job_id"], "engine": "wasm", "input_sha256": id, "module_url": p.Modules[node]})
	}

	if remote["authorization"] != "allow" || remote["error"] != nil || remote["node_id"] != "viii-b" ||
		remote["execution_id"] == nil || remote["execution_id"] == local["execution_id"] ||
		remote["input_sha256"] != id || local["input_sha256"] != id ||
		remote["worker_sha256"] != local["worker_sha256"] || remote["result_sha256"] != local["result_sha256"] ||
		!reflect.DeepEqual(local["result"], remote["result"]) ||
		!bytes.Equal(fixture, readSpikeFile(t, filepath.Join(service.root, id, "original.xml"))) {
		t.Fatalf("remote result or provenance mismatch: %v", remote)
	}
}
