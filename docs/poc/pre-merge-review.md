# PR #10 review guide and merge gates

Scope: synthetic architecture spike, not production readiness. ADR-0001 remains
**Proposed** and issue #9 remains open. Earlier measurements and their limitations
remain in [the recovery report](authorized-execution-recovery.md) and
[executor comparison](executor-comparison.md).

## Reading map

1. [main.go](../../cmd/desktop/main.go), `main`: creates the Wails window and
   local HTTP asset handler; loads optional trusted placement JSON. Evidence lives
   at `%APPDATA%\VIII-PoC` on Windows, independently of Bacalhau.
2. [index.html](../../cmd/desktop/assets/index.html): reads the selected XML bytes,
   POSTs `/api/process`, polls pending attempts, filters normalized message bodies,
   displays selected attributes and the complete original XML using `textContent`.
   Source linkage is chat ID, message ID and `source_index`, not a byte offset.
3. [process.go](../../cmd/desktop/process.go), `process`: bounds input at 1 MiB,
   hashes original bytes for identity, authorizes distributed placement **before**
   normalization/import/staging/submission, then calls `ufdr.Normalize`.
   `Placement.Authorize` in [lifecycle.go](../../internal/wasijob/lifecycle.go)
   requires the exact digest/node allow entry, module URL, worker digest and API.
   Bacalhau node admission alone does not grant evidence access.
4. `keepEvidence`, `save`, `load`: preserve original bytes under the digest;
   store initial attempt intent and immutable numbered observation snapshots.
   Records include request/retry lineage, configuration, input/output identity,
   worker metadata, timestamps, job/execution/node IDs and diagnostics.
   `atomicWrite` syncs a temporary file before rename. Readers ignore temporary
   files; errors propagate. A malformed committed snapshot produces an error,
   rather than silently returning an older observation.
5. Native execution returns the shared normalized result immediately. Distributed
   execution persists intent first, uses inline input or authorized temporary
   staging, then submits the single-task spec in
   [job.go](../../internal/wasijob/job.go). The
   [worker](../../cmd/worker/main.go), `run`, checks the supplied input digest and
   calls the **same** [Normalize](../../internal/ufdr/normalize.go). Implement new
   supported normalization behavior there, with fixture tests; do not fork it
   into the desktop or worker. The current parser contract stays narrow.
6. `inspect` reconciles by recorded job ID or deterministic attempt job name.
   An uncertain submission is retained and looked up, never blindly resubmitted.
   Completed jobs must pass `Description.Result` (one execution, authorized node,
   output present, zero exit, no truncation/error) and `acceptResult` (bounded
   gzip decode where configured; exact expected normalization). Terminal job state
   alone is insufficient. Failed/Stopped/Cancelled/Canceled become local terminal
   records; unknown states remain pending. GET can persist a new observation.
7. DELETE on a nonterminal attempt persists cancellation intent and sends Stop;
   the next observation determines termination. The UI currently has no cancel
   or retry controls. Explicit API retry requires a failed/stopped attempt of the
   same evidence/mode, creates a new attempt, retains request ID and `retry_of`;
   repeated retry requests return the existing attempt.

## Review conclusions and boundaries

No broad refactor or implementation fix was justified by this review. Keeping
the serialized HTTP lifecycle and file persistence together in `process.go` makes
the ordering constraints inspectable and testable. `wasijob` owns the small wire
contract; it is not a competing scheduler. Benchmark-only Docker helpers have a
demonstrated caller and do not enable Docker in the desktop.

The intended stable guarantees are fail-closed placement, byte preservation,
source linkage, explicit retry lineage and independent provenance. The UI,
file snapshot schema, trusted network staging and Bacalhau 1.9.5 API assumptions
are experimental. WorkerSHA256 is recorded from trusted configuration; this is
not cryptographic attestation of downloaded/executed code. A single authorized
execution is accepted; multiple executions are deliberately rejected even if a
future scheduler retry succeeds. No automatic failover guarantee is made.

Nonblocking follow-up proposals:

- **Quality policy:** decide required checks/branch protection, dependency update
  policy, broader package coverage and maintainability thresholds separately.
  Coverage here is descriptive; no mutation framework or arbitrary threshold.
- **Desktop lifecycle:** completed history is not automatically reloaded; pending
  pointers alone are in localStorage. Add discoverable history/cancel/retry only
  after deciding product scope. Failed requests can leave the previous selection
  visible; reviewers must use the displayed evidence ID to avoid misattribution.
- **Persistence ownership:** mutex covers one service instance, not concurrent
  desktops; no directory fsync or power-loss durability proof. Use one instance.
  Temp-write interruption is tested, not OS crash/power-loss injection.
- **Distributed trust:** authenticated staging/artifact return, module verification
  and larger output handling need their own design. Current output is bounded by
  Bacalhau's 2 KiB stdout. These restrict ADR acceptance/production use, not this
  explicitly trusted synthetic spike. Clean installation, managed runtime/update
  lifecycle and physical-host validation remain ADR acceptance blockers.

## Reproduce the automated baseline

Use Go **1.27.4**, Wails **v3.1.0** from go.mod. Linux default Wails compilation
requires CGO, a C compiler, pkg-config, GTK4 and WebKitGTK 6.0 headers
(`libgtk-4-dev libwebkitgtk-6.0-dev` on Ubuntu 24.04). Windows amd64 desktop
compiles with CGO disabled; running it requires WebView2. Race detection runs on
Linux amd64 with CGO enabled. WASI compilation needs neither desktop headers nor
a Bacalhau installation. CI pins Node **22.14.0** for its preview1 runtime check.

```sh
git diff --check
gofmt -l $(git ls-files '*.go')  # expected: no files
go mod verify
go vet ./...
go test -count=1 -v -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
go test -race -count=1 ./...    # Linux/CGO
go build -trimpath -o bin/viii.exe ./cmd/desktop # Windows
GOOS=wasip1 GOARCH=wasm CGO_ENABLED=0 go build -trimpath -o bin/worker.wasm ./cmd/worker
go build -trimpath -o bin/worker-native ./cmd/worker # .exe on Windows
node scripts/verify-worker.cjs
```

In PowerShell use `$env:GOOS='wasip1'; $env:GOARCH='wasm'`, build, then restore
these variables before native commands. Quote `'-coverprofile=coverage.out'`.
[quality.yml](../../.github/workflows/quality.yml) runs on every PR and pushes to
trunk/poc branches; artifacts contain tests.log, coverage and both binaries.
The Node runtime test proves compatible WASI execution, not Bacalhau integration.
`TestDistributedSpike` is opt-in and normally **SKIPPED**, never integration success.
Dedicated infrastructure reproduction remains `scripts/spike-distributed.ps1
-Recovery` with prerequisites in [the existing report](authorized-execution-recovery.md#reproduction-on-one-host).

| Guarantee | Test evidence |
| --- | --- |
| Missing/invalid authorization; no submission or staging on denial | `TestPlacementPolicy` |
| Unauthorized node cannot return an accepted result | `TestCompletedWorkerValidation/unauthorized`, `TestReconcileLostSubmissionResponse` |
| Original bytes and identity/history survive reconstruction | `TestImportPersistenceAndRetry`, `TestSnapshotFailurePreservesHistory` |
| Corruption and invalid storage identifiers rejected | `TestEvidenceIntegrityAndIdentifierBoundaries`; worker contract script wrong-hash case |
| Native/WASI normalization and source references | `TestFixture`; `scripts/verify-worker.cjs` against expected fixture JSON |
| Completed/nonzero, missing/truncated/malformed/mismatched outputs rejected | `TestCompletedWorkerValidation` |
| Reconciliation never resubmits; explicit retry lineage/idempotency | `TestReconcileLostSubmissionResponse`, `TestExplicitRetryLineage` |
| Failed persistence/interrupted temp files preserve valid history | `TestSnapshotFailurePreservesHistory` |

## Windows manual verification (human sign-off pending)

Use one desktop instance and only
`docs/research/fixtures/ufdr/messages-minimal.xml`. Record reviewer, date, commit,
Windows/WebView2 versions and observations/screenshots alongside the review.

- [ ] Launch `bin/viii.exe`. Expect a usable VIII window, no orchestrator needed.
- [ ] Select fixture, Native local, Import and process. Expect evidence digest
  and one result; compare fixture SHA256 with the status and stored directory.
- [ ] Search `library`, select the message. Expect `synthetic-message-0001`,
  chat `synthetic-chat-0001`, source_index `13`, sender Alex Example, recipient
  null, timestamp `2025-01-15T12:00:00+00:00` and the fixture body.
- [ ] Inspect complete original XML, find the matching nested message by those
  identifiers. Compare `%APPDATA%\VIII-PoC\<digest>\original.xml` with fixture.
- [ ] Restart. Completed results are not automatically restored. Reimport the
  same fixture; expect the same evidence ID, unchanged original, another attempt
  JSON with the earlier history retained. For an already configured distributed
  pending attempt, restart should reconcile its stored pointer without resubmit.
- [ ] Select Authorized Bacalhau WASI without placement configuration and process.
  Expect `evidence placement denied` and ability to select Native and recover.
  No staged evidence or job should be created. Do not mistake old selection for
  a new successful result. Optionally repeat WASI only with configured environment.

Automated launch on 2026-11-16 logged Wails v3.1.0/Go 1.27.4 and successful
WebView2 157.0.4341.46 initialization on Windows 11 build 26200, but exposed no
targetable window. Computer Use launch was rejected as not approved to control
VIII. Logs: local `.poc-runtime/quality/ui.stderr.log`; this is **not** a UI pass.

## Merge gates

Local results below use the additions on reference head `1c9d5a7`; GitHub run/head
evidence is recorded in the PR description. No unchecked gate counts as passed.

- [ ] **CI passes on actual PR head — NOT VERIFIED** until linked successful
  Actions jobs are observed. Missing/failing actual-head results block merge.
- [x] **Critical invariants — PASS**: the table's deterministic tests and WASI
  contract script pass locally. Infrastructure spike skipped; live integration
  was not repeated. That limitation does not block this experimental PR.
- [x] **Windows desktop/WASI builds — PASS** locally with Go 1.27.4. Commands
  above; binaries in `.poc-runtime/quality/viii.exe` and `bin/worker.wasm`.
  Hosted runner verification must also pass the CI gate.
- [ ] **Human UI smoke — NOT VERIFIED**: checklist pending; launch logs are
  insufficient. Blocks merge until a human records the observed journey.
- [x] **Architecture review — PASS**: focused review above; no demonstrated
  implementation blocker found. Deferred boundaries are explicit and do not
  prevent review of the synthetic spike. ADR acceptance remains separately blocked.

Recommendation: **Ready for human review**, with CI and human UI gates required
before merge. Do not merge, close #9 or accept ADR-0001 automatically.
