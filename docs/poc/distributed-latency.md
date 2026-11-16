# Distributed execution and latency scaling

The [executor comparison](executor-comparison.md) follows this WASI baseline with native Linux and jobs submitted through Bacalhau's Docker engine.

The runs took place on 2026-11-15 in America/Fortaleza. JSON timestamps use UTC.

## Reproduction

Run from the repository on Windows with Go 1.27.4, the pinned Windows Bacalhau v1.9.5 executable and Docker Desktop in Linux-container mode. The script pulls the Linux image by digest if needed. It builds the UFDR and benchmark modules for `wasip1/wasm`.

```powershell
# Admission, VIII policy, placement and provenance only
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/spike-distributed.ps1 -PlacementOnly

# Placement plus the complete latency matrix
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/spike-distributed.ps1
```

Use `-Short` for a smoke run of the core matrix or `-ScaledOnly` to run just the WASI-calibrated duration sweep. Set `-Results` to a fresh directory to choose where to save the run. `-Bacalhau` and `-Go` override the binary paths. If Docker is unavailable, `-NativeNodes` starts two isolated Windows processes and labels that run separately. The reported Docker run used the default topology.

The script starts a fresh hybrid Windows node `viii-a`, using the existing Bacalhau binary. The second compute node `viii-b` runs the pinned Linux image without a Docker socket or published ports. Both report WASM as their sole execution engine. The Windows API, NATS and staging server bind to loopback on ports 32234, 34222 and 38080. An additional loopback API port 32235 is reserved for the optional native B. Docker Desktop connects to the orchestrator and staging server through `host.docker.internal`. Staging serves only a fixed list of synthetic artifacts. The fixture itself travels inline in approved jobs. The local results publisher is disabled. The script stops the processes and container it started and leaves the run directory for inspection. After an interrupted run, use `owned.jsonl` to identify the script-owned process IDs, start times and Docker container before stopping any survivors. Existing runtime and evidence directories are outside this cleanup.

The Windows runtime build metadata identifies revision `3a6a73aa6529755ac2c9d96a771f0566ff3ee5da`, SHA-256 `6ee846f8352246be157bb6e502185277438c22781201f3635a50354dd0228964`. The Linux image advertises that revision and v1.9.5 and was pulled using the Linux/amd64 manifest digest `sha256:903c4052a4522720adf43d428bdbb267cb24dec5039a759e27dffbcbbd68d878`. The host used Windows build 26200, Intel i5-12450HX, 12 logical CPUs, 16,890,322,944 bytes of RAM and Docker Engine 28.2.2. The Docker environment reported 12 logical CPUs and 8,182,235,136 bytes of RAM. The pinned source defines the [orchestrator REST routes](https://github.com/bacalhau-project/bacalhau/blob/3a6a73aa6529755ac2c9d96a771f0566ff3ee5da/pkg/publicapi/endpoint/orchestrator/endpoint.go), [built-in node labels](https://github.com/bacalhau-project/bacalhau/blob/3a6a73aa6529755ac2c9d96a771f0566ff3ee5da/pkg/node/labels.go), and [URL-download mount behavior](https://github.com/bacalhau-project/bacalhau/blob/3a6a73aa6529755ac2c9d96a771f0566ff3ee5da/pkg/storage/url/urldownload/storage.go).

## Two-node execution

The live test invokes the existing `POST /api/process` handler with a spike-only adapter, the repository UFDR worker and isolated evidence storage. Stock v1.9.5 supplies the persisted node ID as the `name` label. The job uses a required `in` constraint and the client verifies the returned execution node ID. The orchestrator automatically admits its own hybrid compute. With `Orchestrator.NodeManager.ManualApproval` enabled, B registers pending. After an explicit reject it becomes rejected/disconnected, and a synthetic admission probe selecting B has zero executions. The test then approves B and waits for it to reconnect.

The VIII allowlist matches the SHA-256 evidence ID plus the requested node ID. The handler can preserve the original locally. Job construction, inline encoding and submission are downstream of the policy. The live denial keeps B approved/connected and compares the orchestrator's job IDs and staging request log before and after the handler call. The policy unit test also verifies zero adapter calls on denial. The allow case queries each accepted job to verify the node constraint, WASM engine and exact inline fixture bytes. Each run retains its own timing and provenance record.

This experiment treats the loopback orchestrator and the controlled nodes as trusted. The worker hash identifies the local staged artifact. The spike does not attest to the bytes independently from inside the executor. The shared node token and manual admission address cluster membership. Evidence authorization remains an application policy checked at submission. Cross-host network behavior, hostile nodes, callers bypassing VIII, cache invalidation and revocation after submission are outside the run.

## Latency methodology

The benchmark computes SHA-256 over deterministic bytes and runs a fixed 64-bit xorshift chain, with the same input and iteration count on all paths in each cell. The core duration sweep targets 10 ms, 100 ms and 1 s in native Go, calibrated from three native measurements. The supplemental sweep targets 10 ms, 100 ms, 1 s, 10 s and 60 s in WASI, using a common iteration rate derived from a pilot job on each WASI node. Matching native runs use those same iteration counts. This keeps the inputs and amount of work fixed within each comparison while producing useful observations at both ends of the time range. Actual compute time is reported separately. Every sample verifies the worker's input SHA-256 and final checksum against in-process Go. The in-process run reads an existing memory buffer, the native executable reads a local file, and both WASI nodes download the input and worker through HTTP and time file read and computation inside the worker. All three WASI input sizes use URL staging, since base64 for 1 MiB already exceeds the observed NATS message budget. In v1.9.5, `urldownload` appends the URL basename to the mount target, so the benchmark reads `/inputs/<basename>`.

The matrix varies CPU duration at 10 KiB and concurrency 1, input size at the 10 ms target and concurrency 1, and concurrency at the 100 ms/10 KiB baseline. Each cell receives a single warm-up, with ten measured jobs per path for the short cells and two at each long target. The sequential c=1 batch measures ten jobs one at a time for its short cells. The c=2 and c=4 batches use bounded clients and measure throughput as successful jobs per batch wall second. First-use results are excluded from warm summaries. The core `first-use` results measure first use of the benchmark worker after topology validation and native calibration. In a standalone scaled sweep, the WASI pilot is marked `first-use-calibration`. A machine-cold series would require a separate experiment.

Raw samples include wall time, preparation, REST submission, aggregate wait/retrieval, final status fetch, decode, worker file read and compute time. The driver polls every 100 ms. Scheduling, queueing, staging on the compute side, WASI compilation/instantiation and polling are combined in the wait interval. Staging logs measure HTTP handler service time; that excludes node-side storage sync and client readiness. Each sample uses a distinct job ID, and the driver verifies the execution count on completion. There are no driver retries. A timed-out accepted job receives a bounded stop request. Sample summaries give median, nearest-rank p95, min, max and median absolute deviation. The p95 of a two-sample cell is just the observed maximum.

Resource samples record cumulative process CPU seconds and working set/private bytes for each observed Windows process, and Docker's container CPU/memory stats. The initial ten seconds after calibration provide a cluster-ready idle interval before the first worker sample. Subprocesses can finish between resource snapshots. Docker percentages and Windows working sets have different denominators and scopes. Docker Desktop's VM overhead is not attributed to the container.

## Observations

### Placement and transport

| Attempt | Job ID | Execution ID | Application attempt |
| --- | --- | --- | ---: |
| A (Windows) | `j-12d6b2b4-6b1a-4ec6-80d5-b29fa4c10094` | `e-2042b390-3021-4166-9492-62bf49089929` | 1.607 s |
| B (Linux/Docker) | `j-341ab686-ac4b-469f-98e8-4e5c288c7638` | `e-88aab79f-3778-4342-adc8-d29106e1443a` | 1.904 s |

Both attempts recorded worker SHA-256 `6787125be6ca5d7d38d42d52f578367cb44056cd8a134e4fc03b8addaeacdae1`, input SHA-256 `39242c2957b9a488b9b8d32b4c66b496ea3b5ee7b0f51f08e9ed24d92fa636e8` and raw stdout SHA-256 `dabfbdd4f2590b5c9edd53a020f490f915be7eb538bb28e2f145fd832ad46a1f`. The native response had the same normalized content and a different byte hash because the worker adds a newline. The application test also verified the original persisted bytes.

After rejection, probe `j-f28990c1-1ab7-4c9e-9cee-7e2fca6a62ba` failed with zero executions. During VIII's separate evidence denial, job count stayed at two and the staging request count stayed at two. A and B executed only after their respective allow decisions. At runtime, the host listeners were verified at `127.0.0.1:32234`, `127.0.0.1:34222` and `127.0.0.1:38080`.

### Latency and throughput

Core serial medians in milliseconds, shown as elapsed / compute. Each cell contains ten successful measured samples.

| Run | In-process | Native subprocess | WASI A | WASI B |
| --- | ---: | ---: | ---: | ---: |
| CPU target 10 ms, 10 KiB | 9.9 / 9.9 | 19.4 / 10.2 | 1,804.8 / 172.7 | 1,718.2 / 165.2 |
| CPU target 100 ms, 10 KiB | 99.4 / 99.4 | 109.6 / 100.1 | 3,261.7 / 1,716.9 | 3,263.0 / 1,656.1 |
| CPU target 1 s, 10 KiB | 996.6 / 996.6 | 1,006.4 / 996.9 | 19,014.5 / 17,201.4 | 18,254.7 / 16,644.1 |
| CPU target 10 ms, 100 KiB | 10.3 / 10.3 | 20.6 / 10.2 | 1,706.8 / 177.6 | 1,710.2 / 172.2 |
| CPU target 10 ms, 1 MiB | 10.4 / 10.4 | 20.8 / 10.7 | 1,860.9 / 233.9 | 1,806.3 / 226.7 |

Throughput at the 100 ms native-calibrated target and 10 KiB, in jobs/s. Each group ran ten jobs, with one active job at c=1.

| Concurrent clients | In-process | Native subprocess | WASI A | WASI B |
| --- | ---: | ---: | ---: | ---: |
| 1 | 10.05 | 9.07 | 0.31 | 0.31 |
| 2 | 19.88 | 18.13 | 0.55 | 0.57 |
| 4 | 32.88 | 28.80 | 0.84 | 0.84 |

WASI throughput grew about 2.7 times from c=1 to c=4. Median elapsed time per job rose from 3.26 s on both nodes to 4.11 s on A and 4.07 s on B. Worker compute also rose, and median elapsed minus compute grew to 2.04 s and 2.17 s respectively. This is consistent with contention and sublinear throughput. The traces cannot attribute the increase to queueing alone.

WASI-calibrated serial medians in milliseconds, shown as elapsed / compute. All paths in a row performed exactly the same work.

| WASI target | In-process | Native subprocess | WASI A | WASI B |
| --- | ---: | ---: | ---: | ---: |
| 10 ms | 0.55 / 0.55 | 9.64 / 0.54 | 1,505.4 / 10.4 | 1,704.8 / 9.9 |
| 100 ms | 5.51 / 5.51 | 14.94 / 5.81 | 1,905.8 / 97.7 | 1,704.8 / 94.0 |
| 1 s | 56.44 / 56.44 | 67.78 / 56.64 | 2,609.9 / 978.6 | 2,506.4 / 936.2 |
| 10 s | 563.0 / 563.0 | 587.0 / 575.2 | 11,307.4 / 9,795.3 | 11,057.3 / 9,400.6 |
| 60 s | 3,390.2 / 3,390.2 | 3,420.3 / 3,408.0 | 60,246.3 / 58,227.9 | 58,062.9 / 56,474.4 |

Three early native samples recorded zero elapsed or compute time despite valid work outputs. The table excludes those timings, using nine samples in the 10 ms in-process and subprocess cells and the 100 ms in-process cell. The archived JSONL preserves the original samples and original summaries. The updated driver retains these samples with a timing exclusion flag and omits them from latency summaries.



For the short CPU baseline, first-use wall was 10.3 ms in-process, 31.8 ms in a native subprocess, 1.71 s on A and 1.81 s on B. Compared with the warmed medians, only the subprocess showed a clear difference in this pair of observations. The initial 22 native samples and 21 failed WASI attempts are retained separately. Those WASI jobs reported Bacalhau Completed with worker exit 1 because the benchmark attempted to read the folder mount. The corrected spec reads the file below that mount. An earlier startup attempt also returned HTTP 409 when explicitly approving the already-approved hybrid node; the script now checks its membership.

The core timings are consistent with a substantial per-job cost in sequential WASI runs. The median elapsed-minus-compute interval for the three CPU levels was approximately 1.49–1.67 s while worker compute grew from about 0.17 to 17 s. The in-process comparison also shows that the same bit-arithmetic workload spent roughly 17 times longer in WASI compute. That ratio is specific to this kernel, toolchain and runtime.

Changing 10 KiB to 100 KiB and 1 MiB changed the server-side GET time and time spent hashing bytes. Sequential elapsed minus compute did not increase monotonically. The logs show fresh GETs for the same worker and input across successive jobs. The run establishes repeated downloads. It does not quantify OS page cache, Wazero compilation caches or a true cold-start penalty. The Docker and Windows nodes used comparable same-host paths. The Linux results cannot predict different hosts or WAN costs.

Median REST submission per WASI core cell was 3.7–20.5 ms, and median final poll request took 0.6–1.0 ms. In the approximation `T_total = T_compute + T_transfer + T_queue + T_fixed + T_other`, worker compute is measured directly. The other terms share an elapsed-minus-compute interval that also includes polling. The measurements support an approximately fixed sequential component under this topology, with a larger residual under concurrent load. Median HTTP handler service time for input GET was about 0.52 ms for 10 and 100 KiB and 2.36 ms for 1 MiB. The 4.85 MB worker's service time was about 93 ms. These handler measurements do not include client-side processing.

### What the overhead shares support

- **Below 10%.** The median per-sample residual share was still 13.4% on A and 15.0% on B at about 9–10 s of WASI compute. At roughly 17 s it was 8.8% on both. These observed medians bracket the 10% threshold in this sequential scenario.
- **Below 5%.** At about 56–58 s of WASI compute, the median share fell to 3.35% on A and 2.74% on B, with two measured samples per node. This establishes an observed regime below 5%. A sharper crossover estimate would need more observations between the tested durations.
- **Below 1%.** This share was outside the observed range. Longer jobs and a more precise timing decomposition would be needed to estimate it.

Shares use elapsed minus measured WASI compute, divided by elapsed. They describe the time outside the worker's measured computation. For the same work in the 60-second WASI cell, native Go took about 3.4 s total. The share of time outside the WASI worker and the end-to-end cost relative to native Go answer different questions. The report uses measured points and avoids extrapolating from the longest cells.

### Resource footprint

Across the two cluster-ready idle windows, the Windows hybrid's median working set ranged from 68.0 to 129.6 MiB and CPU from 1.5% to 2.3% of one core. Staging used about 48.5 MiB and showed no CPU increase at that resolution. The Docker compute container used about 94.0 MiB and reported roughly 1.9–2.0% CPU. The benchmark driver itself held about 63.8 MiB while idle. The subprocess uses the same native executable, which also links the benchmark driver and its REST client. Its binary and memory footprint differ from a worker-only production build. Native processing does not need the staging or Bacalhau daemons. Wails and the Docker Desktop VM overhead were outside the sample scope.

During the roughly 17-second WASI jobs on A, the Windows hybrid's median working set was 170.2 MiB and relative CPU rate was about 100.5% of one core. During corresponding B jobs, the compute container reported 116.8 MiB and 101.3% median CPU. The Windows hybrid still handled remote orchestration at about 3.0% of one core and 119.4 MiB median working set in those windows. In active WASI windows of the scaled sweep, which included jobs near 60 seconds, A's Windows process and B's container showed median memory of 184.1 MiB and 149.6 MiB, respectively. At the WASI-calibrated 60-second target, the two native subprocesses used 3.42 and 3.30 CPU-seconds. Snapshot sampling does not reliably capture every short subprocess. The numbers come from one shared machine, and Docker accounting and Windows working set are distinct measures.

### Raw results

- [Placement and policy](results/issue-9-distributed/placement.jsonl)
- [Core latency samples](results/issue-9-distributed/latency-core.jsonl), [resources](results/issue-9-distributed/resources-core.jsonl) and [staging](results/issue-9-distributed/staging-core.log)
- [WASI-calibrated latency samples](results/issue-9-distributed/latency-wasi-scaled.jsonl), [resources](results/issue-9-distributed/resources-wasi-scaled.jsonl) and [staging](results/issue-9-distributed/staging-wasi-scaled.log)
- [Initial run with mount errors](results/issue-9-distributed/latency-initial-failed.jsonl)

The core file contains all 28 core cells and 280 measured jobs. Its original plan also listed two larger native-calibrated workloads. After the core cells completed, that driver was stopped before those rows collected samples. A fresh cluster ran the separate WASI-calibrated sweep with 20 cells and 136 measured jobs. All 416 jobs across the two corrected runs returned matching digests and checksums. The three zero-timing observations described above leave 413 usable timing samples. The corrected runs recorded no job failures, timeouts or duplicate executions. The current script runs both sweeps by default and can reproduce the scaled sweep on its own. The 10-minute optional workload was outside the run budget.
