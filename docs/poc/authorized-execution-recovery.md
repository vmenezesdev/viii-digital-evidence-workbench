# Issue #9 validation — authorized UFDR execution and recovery

Date 2026-11-16 (America/Fortaleza). Raw records use UTC.

**Recommendation — Revise ADR-0001. Keep its status Proposed.** Keep native Go for workstation processing. Retain Bacalhau as a candidate for authorized distributed execution, with the application owning evidence access, the original bytes, and attempt lineage.

## Workload and placement boundary

The generator emits 32 deterministic fictional messages by default. Each record has a distinct ID and source index, a body, the fixture timestamp, and a sender nested in the supported UFDR Chat structure. The integration test checks every normalized message and compares input and output SHA-256 across native Go, A, and B. The worker also checks the input digest before processing. The deliberately repetitive message bodies exercise the volume and placement path. They represent neither corpus diversity nor a production parser benchmark.

A Bacalhau request needs an exact evidence digest and an explicit node ID in VIII's trusted placement configuration. The handler performs this check before importing, staging, or submitting the request. The authorization decision pins the job's built-in `name` selector to the one allowed node ID. VIII independently checks the reported execution identity before accepting output. The desktop defaults to local native Go. Launch `viii.exe -placement path-to-policy.json` to enable explicit Bacalhau placements.

The harness creates a private staged file only after authorization. It mounts the downloaded file under `/inputs`, uses an attempt-specific URL and removes the file after terminal reconciliation. The orchestrator and node API bind to loopback. Docker Desktop reaches the loopback staging service through `host.docker.internal`. The stage and orchestrator are trusted PoC components. An attempt URL acts as a bearer capability. Keeping evidence private against untrusted peers or log readers requires authenticated staging and protected access to the orchestrator.

Bacalhau 1.9.5 returns at most 2 KiB of worker stdout in its description API. The staged workload uses gzip/base64 for that field with bounded decode in VIII. A 48-message trial exceeded the return limit even though staging worked. The 32-message trial retains the staged workload and fits this output contract. Larger or less compressible normalized results require an authorized artifact return path. The prototype leaves Bacalhau's transport limits unchanged.

## Observed on one physical workstation

The isolated Windows processes used independent data directories. Bacalhau refused the admission probe for rejected B. The probe failed with zero executions. With B subsequently approved and connected, the handler rejected access with HTTP 403. The orchestrator job list stayed at two entries and the staging request count stayed at four. Allowing the exact digest for B enabled execution there.

| Event | UTC | Node | Bacalhau job | Execution |
| --- | --- | --- | --- | --- |
| Authorized run on A completed | 05:19:13.673 | `viii-a` | `j-4ac0e86a-3d7e-41d0-95eb-43b6342785a9` | `e-9242b1a1-a725-4664-87d4-d036bafb3881` |
| Authorized run on B completed | 05:19:16.015 | `viii-b` | `j-856fcf43-ccf5-46e6-853f-3e7361645e21` | `e-64f00f63-4907-4d8c-a574-0d552a75b3dc` |
| B killed after worker normalized input | 05:19:40.860 | `viii-b` | `j-8fa3ce44-f778-4395-bf36-e5bed4fa23fa` | `e-85e847e7-ccf3-48b0-97cc-fb4c000f2f0f` |
| Interrupted attempt reconciled as Stopped | 05:20:51.164 | `viii-b` | `j-8fa3ce44-f778-4395-bf36-e5bed4fa23fa` | `e-85e847e7-ccf3-48b0-97cc-fb4c000f2f0f` |
| Empty-report control accepted by A after B died | 05:20:52.200 | `viii-a` | `j-0e88c63c-ef19-4bfc-b2f1-ec1954c258eb` | `e-328f6419-b67d-45fd-a526-991bf79199e9` |
| Explicit retry completed | 05:20:57.950 | `viii-a` | `j-2abe2a38-d7cd-4f38-981c-08a1f7114c7d` | `e-ae5adc00-ab91-4a3a-8e6a-8ef103d58f6f` |

The pinned job stayed Running for the 70-second observation window, including after the orchestrator marked B disconnected. Its configured task timeout was 135 seconds. One execution remained linked to B. VIII sent cancellation and observed Stopped on its next successful read. A separate cancellation test stopped an accepted job on the live A node. The empty-report control had two eligible nodes at failure time. Bacalhau created and accepted a fresh execution on A under the original job ID. The harness cancelled the control after observing acceptance, so it establishes rescheduling and leaves completion of that control unverified.

The successful native, A, B and manual retry results all match. Input was 113,593 bytes with SHA-256 `e2138dab0bb6b6dfbc87bf8fe3cc3c05cdbad528f2912f6073ea0a8220354132`. Normalized JSON SHA-256 was `59097d0981cf5bffaaebff200207baa8afdb84968251d7f1c83234988765d26b`. The built WASI artifact SHA-256 was `913de960c8e89aff17ab3f918bca95f985371e578f4ade826446e9b582abdd6f`.

Before and after failure, a reconstructed VIII service found the same attempt and Bacalhau job ID. Reposting the active request and reconciling state left the submitted job list unchanged. The explicit retry retained the request ID, linked the interrupted attempt, and obtained its own job and execution IDs. The original bytes, evidence directory count of one, prior attempt record and recorded worker digest remained intact. No staged files remained. [Windows events](results/issue-9-recovery/windows-native.jsonl) · [orchestrator excerpts](results/issue-9-recovery/windows-native-orchestrator.log) · [staging log](results/issue-9-recovery/windows-native-staging.log).

The second run put B on Docker Desktop's Linux engine (28.2.2) and kept A on Windows, on the same physical workstation. The input, artifact and output hashes matched the Windows run. During the VIII denial, the job list stayed unchanged and staging requests stayed at four. Bacalhau moved the pinned job from Running to Failed 72.6 seconds after the kill. The orchestrator logged the configured 70-second task timeout and one available node with zero suitable nodes. A did not match the B-only selector. There was no replacement execution. VIII persisted the failure and the original B execution, and the manual retry completed on A. The runs used different task timeouts and observation windows, so they do not establish a topology-dependent failure timing.

| Windows / Docker event | UTC | Node | Bacalhau job | Execution |
| --- | --- | --- | --- | --- |
| Authorized run on A completed | 05:30:00.223 | `viii-a` | `j-e695d847-d23b-4467-8d06-f038da238494` | `e-f64e3a21-63b1-49ba-82b5-89687c383aa5` |
| Authorized run on B completed | 05:30:02.864 | `viii-b` | `j-2267e5eb-0fe9-41a8-9727-a535233dfb4d` | `e-41c8cf0d-bbbf-47ab-a064-303435826e4a` |
| B killed after worker normalized input | 05:30:06.001 | `viii-b` | `j-8fd2090a-6bef-4cc5-bf13-4123143e179f` | `e-e3cc662f-ab61-4a0a-9c96-de5c4bea31e5` |
| Interrupted attempt reconciled as Failed | 05:31:18.645 | `viii-b` | `j-8fd2090a-6bef-4cc5-bf13-4123143e179f` | `e-e3cc662f-ab61-4a0a-9c96-de5c4bea31e5` |
| Explicit retry completed | 05:31:22.769 | `viii-a` | `j-2d646e0b-f2ec-45b8-b2c2-95ffdb0c2b62` | `e-bbc56fe8-6b7e-47e2-9c8b-924e04b0976e` |

The Docker run also verified Stopped after cancellation of a separate accepted job. The DELETE directed at the already-failed attempt returned its terminal record. [Windows/Docker events](results/issue-9-recovery/windows-docker.jsonl) · [orchestrator excerpts](results/issue-9-recovery/windows-docker-orchestrator.log) · [staging log](results/issue-9-recovery/windows-docker-staging.log).

## Reproduction on one host

Use PowerShell on Windows with Go 1.27.4 and the Bacalhau Windows v1.9.5 binary (revision `3a6a73aa6529755ac2c9d96a771f0566ff3ee5da`). The Docker configuration uses Docker Desktop's Linux engine and the image digest pinned in the script. Each run needs a fresh results directory and ownership of loopback ports 32234, 32235, 34222 and 38080 for the duration of the experiment.

```powershell
& 'C:/Program Files/Go/bin/go.exe' test -count=1 ./...
& ./scripts/spike-distributed.ps1 -Recovery -Results .poc-runtime/recovery-docker
& ./scripts/spike-distributed.ps1 -Recovery -NativeNodes -ProbeFailover -Results .poc-runtime/recovery-native
```

The Go suite, `go vet ./...`, desktop build, both recovery runs, and the earlier inline fixture regression with `-PlacementOnly -NativeNodes` passed. Handler tests covered missing policy, missing node, a disallowed node, a different digest, the exact allowed placement, and a lost submit response followed by recovery by job name. The recovery test reconstructed the service against the same disk state. A manual Wails UI walkthrough was outside this run.

Each recovery run invokes the real desktop HTTP handler through Go's test harness and reconstructs the service against the same evidence directory. The script stops B only after capturing an execution-specific stderr marker emitted by the WASI worker after normalization and before its configured pause ends. It ends only the processes or containers it created. The test preserves raw observations and the per-attempt journal in the selected results directory. Use `-PlacementOnly` to run the prior small-fixture placement regression independently.

For standalone desktop configuration, set `Client.Base` to the isolated orchestrator API, `Allow` to a map of evidence digests to permitted node IDs, `Modules` to a map of node IDs to worker URLs, and `WorkerSHA256` to the staged artifact digest. Workloads over 64 KiB also need `StagingDir` set to the stage process's existing `staged` directory and `StagingURLs` set to each node's stage origin. Select the Bacalhau mode in the desktop and enter the authorized node ID. The handler exposes `GET /api/attempt?evidence=<sha256>&attempt=<id>` for reconciliation, `DELETE` on that route for cancellation, and the `retry_of` parameter on `POST /api/process` for explicitly linked retries.

The workload generator can also be run independently with `go run ./cmd/synthetic-ufdr -messages 32`. Save stdout as raw bytes if reproducing a recorded digest. Older Windows PowerShell versions can re-encode text redirected with `>`.

## Reproduction across two physical hosts

**Acceptance gate unverified.** The completed local experiments use isolated compute nodes on a single workstation. A suitable second physical machine was unavailable for this run.

Use the Windows workstation as A and a dedicated Linux host as B. Configure the SSH alias `viii-recovery-b` on A with the verified host key and key-based authentication. On B, permit remote TCP forwarding for the experiment account and keep SSH `GatewayPorts no` so reverse listeners remain on loopback. Reserve loopback ports 32235, 34222 and 38080 on B. Ensure the experiment account can use its dedicated Docker engine. On B, cache the pinned image before starting the test.

```sh
docker pull ghcr.io/bacalhau-project/bacalhau@sha256:903c4052a4522720adf43d428bdbb267cb24dec5039a759e27dffbcbbd68d878
```

From A, verify that the alias refers to B and execute the harness.

```powershell
ssh -o BatchMode=yes -o StrictHostKeyChecking=yes viii-recovery-b hostname
& ./scripts/spike-distributed.ps1 -Recovery -ProbeFailover -PhysicalHost viii-recovery-b -Results .poc-runtime/recovery-physical
```

The runner uses private SSH reverse forwards to the A orchestrator and stage. B joins via its forwarded loopback addresses and runs an isolated Bacalhau container. The runner reads that container's execution log over SSH and kills only the container it started. Record each physical machine's identity separately from the Bacalhau node IDs and retain the full run directory.

## Responsibility and remaining risk

Bacalhau supplies membership admission, scheduler selectors, job and execution identifiers, worker execution, and job observation and cancellation APIs. VIII supplies per-evidence authorization, staging authorization and cleanup, original bytes and hashes, reconciliation, worker/configuration records and explicit retry lineage.

An intent record is persisted before submission. If the submit response is lost, VIII tries to locate the job by the unique persisted name. A missing response and an unavailable lookup leave an unresolved attempt visible and block automatic resubmission. Immutable snapshots preserve observed transitions, including the accepted job ID, worker artifact digest, requested and actual node, processing configuration and outcome. The experiment uses one active VIII instance and reconstructs its service from disk. Abrupt host power loss, concurrent desktop instances, and retention after losing the orchestrator's own persisted store remain open. Worker identity is the digest of the artifact built for this run and served from trusted staging. Production integrity needs verification of the artifact at execution.

The optional distributed path still requires the orchestrator, node provisioning and staging, while the native path runs in the workstation process. The earlier executor measurements also show fixed scheduling and execution overhead. The experiment leaves installation, upgrades, and the operational case for representative cross-host workloads open. [Executor comparison](executor-comparison.md) · [ADR-0001 follow-up](../adr/0001-follow-up-distributed-runtime.md).

The current handler performs local UFDR normalization to validate inputs and establish output parity during reconciliation. This experiment measures placement and recovery behavior. It cannot establish a speedup from moving normalization off the workstation.
