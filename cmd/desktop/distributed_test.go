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

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/ufdr"
	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
)

func callAttempt(t *testing.T, service *workbench, method, path string, input []byte, want ...int) map[string]any {
	t.Helper()
	response := httptest.NewRecorder()
	service.ServeHTTP(response, httptest.NewRequest(method, path, bytes.NewReader(input)))
	for _, code := range want {
		if response.Code == code {
			var record map[string]any
			decoder := json.NewDecoder(response.Body)
			decoder.UseNumber()
			if err := decoder.Decode(&record); err != nil {
				t.Fatal(err)
			}
			return record
		}
	}
	t.Fatalf("HTTP %d, wanted %v: %s", response.Code, want, response.Body.String())
	return nil
}

func attemptPath(a map[string]any) string {
	return fmt.Sprintf("/api/attempt?evidence=%s&attempt=%s", a["evidence_id"], a["attempt"])
}

func processAt(t *testing.T, service *workbench, mode, node string, fixture []byte) map[string]any {
	t.Helper()
	record := callAttempt(t, service, "POST", "/api/process?mode="+mode+"&node="+node, fixture, 200, 202, 403)
	deadline := time.Now().Add(50 * time.Second)
	for record["finished"] == nil && time.Now().Before(deadline) {
		time.Sleep(150 * time.Millisecond)
		record = callAttempt(t, service, "GET", attemptPath(record), nil, 200, 202)
	}
	return record
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
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
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
	recovery := os.Getenv("VIII_SPIKE_RECOVERY") == "1"
	if recovery {
		var generated bytes.Buffer
		if err := ufdr.WriteSynthetic(&generated, 32); err != nil {
			t.Fatal(err)
		}
		fixture = generated.Bytes()
		if len(fixture) <= inlineLimit {
			t.Fatal("workload did not exercise staging")
		}
		if err := os.WriteFile(filepath.Join(root, "synthetic.xml"), fixture, 0600); err != nil {
			t.Fatal(err)
		}
	}
	id := digest(fixture)
	p := &wasijob.Placement{
		Allow: map[string]map[string]bool{id: {"viii-a": true}},
		Modules: map[string]string{
			"viii-a": wasijob.StageURL(os.Getenv("VIII_SPIKE_STAGE_A"), "worker.wasm"),
			"viii-b": wasijob.StageURL(os.Getenv("VIII_SPIKE_STAGE_B"), "worker.wasm"),
		},
		WorkerSHA256: digest(readSpikeFile(t, filepath.Join(root, "worker.wasm"))),
		Client:       client,
	}
	if recovery {
		p.StagingURLs = map[string]string{"viii-a": os.Getenv("VIII_SPIKE_STAGE_A"), "viii-b": os.Getenv("VIII_SPIKE_STAGE_B")}
		p.StagingDir = filepath.Join(root, "staged")
	}
	service := &workbench{root: filepath.Join(root, "evidence"), placement: p}
	native := processAt(t, service, "native", "", fixture)
	local := processAt(t, service, "bacalhau", "viii-a", fixture)
	save := func(event string, record map[string]any) {
		delete(record, "original_xml")
		if recovery && record["result"] != nil {
			copy := map[string]any{}
			for key, value := range record {
				if key != "result" && key != "staging_dir" {
					copy[key] = value
				}
			}
			write(event, copy)
			return
		}
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
	if denied["authorization"] != "deny" || denied["job_id"] != nil || denied["error"] != "evidence placement denied" ||
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
			!expectedSource(sources[1].Source.Type, sources[1].Source.Params["URL"], sources[1].Target, p, fixture, record) {
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
	if recovery {
		assertSynthetic(t, remote)
		testRecovery(t, ctx, service, p, fixture, jobs, write)
	}
}

func expectedSource(kind, address, target string, p *wasijob.Placement, input []byte, a map[string]any) bool {
	if p.StagingDir == "" {
		return kind == "inline" && target == "/input.xml" && address == wasijob.InlineXML(input)
	}
	return kind == "urldownload" && target == "/inputs" &&
		strings.HasPrefix(address, strings.TrimSuffix(p.Modules[a["node_id"].(string)], "/worker.wasm")) &&
		strings.HasSuffix(address, "/evidence/"+a["attempt"].(string)+".xml")
}

func assertSynthetic(t *testing.T, a map[string]any) {
	t.Helper()
	messages := a["result"].(map[string]any)["messages"].([]any)
	if len(messages) != 32 {
		t.Fatalf("expected 32 messages, got %d", len(messages))
	}
	for i, item := range messages {
		m := item.(map[string]any)
		index := i + 1
		from := m["from"].(map[string]any)
		if m["id"] != fmt.Sprintf("synthetic-message-%04d", index) ||
			m["source_index"] != fmt.Sprint(index+11) || m["chat_id"] != "synthetic-chat" ||
			m["body"] != ufdr.SyntheticBody(index) || m["timestamp"] != "2025-01-15T12:00:00+00:00" ||
			from["id"] != "synthetic-party" || m["to"] != nil {
			t.Fatalf("incorrect normalized message %d", index)
		}
	}
}
