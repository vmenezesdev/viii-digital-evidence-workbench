# Issue #9 validation — development spike

> Architecture revision: this is a historical validation snapshot. The [revised ADR and technical trade-off case](../adr/0001-native-execution-tradeoff.md) supersede its native-first recommendation: product jobs use Bacalhau with portable WASI or optional performance-oriented Docker. The application stack is decided separately in [ADR-0002](../adr/0002-adopt-go-wails-v3.md). This revision does not complete the remaining acceptance gates or change the PoC implementation. See [issue #9](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/9) for post-merge validation status.

Date: 2026-11-15 (America/Fortaleza; job records cross into November 16 UTC).

The [authorized execution and recovery follow-up](authorized-execution-recovery.md) documents the current placement configuration and test runner. The commands below describe the earlier development spike.

**Recommendation: Revise ADR-0001. Keep its status Proposed.** Docker-free WASI normalization works on Windows, but the authorized two-node synthetic test now passes on one Windows/Docker Desktop host. Installation, lifecycle, cross-host behavior and updates still need validation. Do not merge it as completion of #9. No public redistribution artifact is supplied.

## Original run environment and identity

Observed Windows 11 Home Single Language 25H2, build 26200, amd64; Go 1.27.4; Wails 3.1.0; Node 22.19.0; npm 10.9.3; WebView2 157.0.4341.46; NSIS 3.13 (absolute path required). `wails3 doctor` passes, although NSIS is not discovered on this shell's PATH. Docker CLI 28.2.2 exists, daemon not running. The Bacalhau engine was explicitly disabled for all runs described here.

`go version -m C:\Bacalhau\bin\bacalhau.exe` identifies module v1.9.5, source revision `3a6a73aa6529755ac2c9d96a771f0566ff3ee5da`, source time `2026-10-12T18:36:15Z`, `vcs.modified=false`, Go 1.27.4, CGO_ENABLED=0, windows/amd64. CLI version is `v0.0.0-xxxxxxx`. The adjacent source checkout has the same HEAD and an untracked `$HOME/` directory. Its current state is not proof of how the binary was built; original build invocation is unknown. Binary size 108,743,680 bytes; SHA-256 `6EE846F8352246BE157BB6E502185277438C22781201F3635A50354DD0228964`.

Go and Wails required absolute paths in this session. Sandbox cache writes initially failed; dependency resolution/builds required execution outside the filesystem sandbox. No dependency reinstallation was performed.

## Plan and reproduction

Sequence: verify binary/source identity → execute WASI probe → native parser/fixture tests → execute real WASI worker → compare outputs → compile desktop and test its application boundary → document remaining gates.

From the repository root in PowerShell:

```powershell
$env:Path = 'C:\Program Files\Go\bin;C:\Users\vinic\go\bin;' + $env:Path
$env:GOCACHE = Join-Path $env:TEMP 'viii-go-cache'
go mod download
go test ./...
go build -o .poc-runtime/viii.exe ./cmd/desktop
```

In a dedicated terminal, start the isolated development node (do not reuse an unrelated node):

```powershell
C:\Bacalhau\bin\bacalhau.exe serve --orchestrator --compute --data-dir .poc-runtime/node2 --api-host 127.0.0.1 --api-port 2234 -c Orchestrator.Host=127.0.0.1 -c Orchestrator.Port=24222 -c Publishers.Types.Local.Port=26001 -c Engines.Disabled=docker
```

In a second terminal run `go run ./cmd/stage`. In a third:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/validate-worker.ps1
$env:VIII_TEST_BACALHAU = '1'
go test -v ./cmd/desktop
.poc-runtime/viii.exe
```

The script builds the worker before submitting it. Stop only these terminals' owned processes with Ctrl+C. The observed node startup log reports `nats://127.0.0.1:4222` despite the explicit orchestrator-port setting; verify effective configuration before coexisting with another cluster. These commands are developer instructions, not a validated no-CLI installation.

Select `docs/research/fixtures/ufdr/messages-minimal.xml`, choose an execution mode, import/process, search `library`, and select the message. **This UI walkthrough has not been manually executed**; compiled code and HTTP-handler integration are the evidence available here. User state is under `%APPDATA%\VIII-PoC`; originals are addressed by SHA-256, with separate timestamp-named processing records and request specifications. This storage is a PoC, not transactional crash-safe persistence.

## A. Windows desktop installation

| Requirement | Status and evidence |
| --- | --- |
| Build/package | Windows Wails executable compiled, 18,078,208 bytes before final diagnostics edit. NSIS found at `C:\Program Files (x86)\NSIS\makensis.exe`; installer not produced. |
| Clean install/first launch without toolchains | Not tested: no clean Windows VM provisioned or inspected. Privileges, uninstall, dependency diagnostics and state removal untested. |
| Managed startup, reconnect, logs and shutdown | Partial: isolated nodes started and connected; first owned node stopped with terminal Ctrl+C. Desktop currently requires developer-managed node/staging processes; no process supervisor. |
| No-CLI first workflow | Not passed: development commands required. UI interaction untested. |
| Network/air gap | Update checks attempted outbound HTTPS and were blocked by sandbox networking. Local worker execution still succeeded. No air-gap or offline-install claim. |
| Redistribution | Not cleared: no runtime bundle, license/asset audit, attribution package, renamed distribution identity, or legal review. Issue comments' licensing assessment is not approval. |

## B. Local Docker-free workflow

| Requirement | Status and evidence |
| --- | --- |
| Synthetic fixture normalization | Passed: `go test ./...` checks semantic equality against expected JSON, altered body parsing, malformed input and unsupported root. Parser is limited to documented hierarchy; it rejects To records rather than extending that contract. |
| Actual WASI execution | Passed: probe job `j-ded52dc4-da42-48f5-a841-2b992947c84b`; real worker job `j-912d2ec4-1817-4fa1-b46b-afb76910e018`, execution `e-b04e3743-7bc3-4064-8a01-b026ad1735fc`, node `n-af8285f3-6f1e-412d-b72e-edf63b3e3156`. Docker engine disabled. |
| Input/output interfaces | Worker v0: XML file path argument, JSON stdout, errors stderr/nonzero exit. WASM fetched over loopback HTTP; original XML supplied as inline base64. `job describe --output json` exposes execution RunOutput. `job logs` stalled and is not used by desktop. |
| Import preservation, identity | Passed at handler boundary: originals preserve input bytes; SHA-256 identity survives two service instances and retries with one evidence directory and two attempt records. Checked-out input hash `39242C2957B9A488B9B8D32B4C66B496EA3B5EE7B0F51F08E9ED24D92FA636E8` differs from SHA256SUMS; `core.autocrlf=true`. No source-byte canonicalization is applied. |
| Search/source inspection | Implemented, UI not tested. Whole original XML is shown alongside selected chat/message ID and source_index. Source index is not used as byte offset. Automated dedicated source-selection test remains missing. |
| Independent provenance | Partial: original hash/identity, request JSON, mode, worker contract version, configuration, local location, timestamps/duration, job/execution/node IDs and result hash saved outside Bacalhau. Exact worker/runtime artifact hashes are not yet captured per attempt; pending-request recovery and atomic commits are missing. |
| Adapter/service boundary | Partial: CLI helper and application handler exist; a dedicated reusable adapter interface is still needed. Native comparison invokes identical Go normalization, without a scheduler. |

Raw inline WASM (4,757,443 bytes) exceeded NATS maximum payload, left the job Pending, and caused repeated publish failures. Compressed tar (1,323,034 bytes before base64) also failed to progress. Job `j-4cb47a1f-41f9-4e5f-8301-75ec0662b3f9` was explicitly stopped; the node was stopped and a fresh isolated node used. Small inline XML plus HTTP WASM staging succeeded. Local filesystem input was rejected: node supports `inline, urldownload`, not `local` by default. No custom executor was introduced.

## C. Authorized distributed execution

**Passed on one host.** An isolated Windows Bacalhau hybrid and separate Linux compute container at v1.9.5 executed the same UFDR WASI contract through the existing VIII handler. Docker execution was disabled on both nodes. The fixture results were semantically equal to native Go and the two WASI stdout hashes matched. The original evidence ID and worker hash matched, and the job, execution and node IDs distinguished the attempts.

Bacalhau manual admission rejected B and a job constrained to that node failed with zero executions. Separately, with B approved and connected, VIII denied placement for the fixture's SHA-256 evidence ID before generating or submitting a spec. The denial added zero jobs and zero staging requests. A subsequent explicit allow executed on B. Accepted specs were checked for the exact inline bytes, WASM engine and required node constraint. The staging service binds to Windows loopback and serves an allowlist of synthetic artifacts; Docker Desktop reaches it via `host.docker.internal`.

[Reproduction, raw records and limitations](distributed-latency.md). Independent hosts, interruption/recovery after acceptance and production-grade authorization and artifact attestation remain open.

## D. Failure recovery and idempotency

| Requirement | Status and evidence |
| --- | --- |
| Restart/retries | Partial pass: native handler test recreates service with same storage and repeats import without duplicate evidence. Complete desktop restart and pending Bacalhau reconciliation not tested. |
| Cancellation | Partial: CLI stop accepted for oversized pending job. Active worker cancellation, UI cancellation and resulting terminal state not validated. |
| Worker failure | Controlled missing-file job `j-3a6f953c-a614-43c0-99b5-e9347f11bd61` returned worker exit 1, stderr `open /missing.xml: No such file or directory`. Bacalhau reported job Completed successfully, so adapter checks RunOutput.ExitCode independently. Worker kill/crash and automatic retries not tested. |
| Diagnostics | Adapter reports command failures, nonzero worker exit and a 45-second deadline; unavailable orchestrator and unreachable node tests not run. Deadline does not cancel an accepted job. |
| Idempotent persistence | Evidence deduplication test passed. Processing attempts deliberately remain separate. Abrupt-write failure safety/concurrent instances not tested or guaranteed. |

## E. Coordinated updates

All requirements **not tested/not implemented**: maintenance admission, protecting pending/active jobs, independent worker upgrade, artifact integrity/compatibility checks, health/smoke tests, failed-update rollback and historical version validation. Wails app updating alone cannot establish coordinated Bacalhau/worker/data migration behavior. No updater or installer artifact is supplied.

## F. Operational measurements and native comparison

The follow-up benchmarks Go in-process, a native subprocess and WASI on Windows and Docker Linux. It holds the input and iteration count constant within each CPU comparison, separates measured worker compute from elapsed time, and varies input size and concurrency in dedicated cells. With one active job, the native-calibrated CPU cells showed about 1.5–1.7 s outside WASI worker computation. The residual grew with four concurrent jobs. The same arithmetic kernel took roughly 17 times longer within WASI than in native Go. At about 56–58 s of measured WASI compute, the outside-compute share was 2.7–3.4%, with two samples per node. See the [latency and resource results, raw samples and reproduction](distributed-latency.md) and the [ADR-0001 follow-up](../adr/0001-follow-up-distributed-runtime.md).

### Earlier single-fixture measurements

Executed `scripts/validate-worker.ps1`: native/WASI semantic JSON equivalence **passed**. Repeat submission job `j-2d0c6c90-739b-4daf-bb4f-df58b21c467c`: submission 566 ms, total submission/polling/completion 3,685 ms. Native process wall samples: 286.1287, 13.2086, 12.5381, 12.0684, 11.8401 ms. First sample includes cold-launch effects. A prior run measured 313.8778, 9.4097, 9.3682, 28.2946, 19.3511 ms. Single tiny fixture, sequential samples, no statistical or throughput claim. In-process desktop native execution is a different timing scope.

Observed Bacalhau working set snapshots: 206,209,024 bytes after transport failures; 97,202,176 bytes later on fresh node. CPU time 13.265625 seconds at latter snapshot is cumulative process time, **not idle CPU utilization**. No reliable idle/processing resource series collected. Native executable 4,294,656 bytes; WASM 4,757,443 bytes; runtime 108,743,680 bytes. Working WASM SHA-256 `80A2368B1E5AAE451FE205F6C30880207F27EAFA023B887B2A3DFF4F58ECD503` (rebuilds may differ).

Development topology adds Bacalhau and staging processes to the desktop; native comparison needs neither. Clean-install time, first-use success rate, second-node steps, update effort and rollback cost remain unmeasured. No claim of acceptable single-workstation overhead is justified yet.

## Decision and remaining gates

Revise the proposal to explicitly account for staged worker distribution, transport limits, worker exit interpretation, and Windows profile-migration privileges. Default native Go is materially simpler for this tiny local workload; retain Bacalhau as a candidate for an explicitly authorized distributed path, subject to further evidence. The same-host test establishes correct placement and provenance. Performance and recovery across hosts remain open.

Do not accept the ADR from these tests. Complete managed lifecycle/cancellation/reconciliation, artifact-pinned provenance, adapter extraction, manual UI validation, cross-host policy and failure tests, clean installer and update/rollback tests before revisiting acceptance. The core execution premise passed; the remaining work was not falsified or completed by this spike.
