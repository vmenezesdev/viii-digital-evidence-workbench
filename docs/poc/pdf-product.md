# PDF product validation on Windows

The small product workflow passed with Bacalhau Windows v1.9.5, revision
`3a6a73aa6529755ac2c9d96a771f0566ff3ee5da`. The packaged app imports an immutable
SHA-256 original, authorizes local placement, runs range jobs, persists each page
and exposes incremental FTS5 search. WASI is the default; Docker is optional.

| Product test | First useful search | Total | Result |
| --- | ---: | ---: | --- |
| WASI | 7.921 s | 165.919 s | 11 searchable, 1 unreadable |
| Docker selection | 8.928 s | 30.160 s | Same physical pages |
| Live WASI interruption, reopen, then Docker | 8.933 s | 36.328 s | Same active attempt on reopen, 8 accepted page hashes unchanged, 12 unique pages |
| Fresh profile, restricted network, no Docker/toolchain on PATH | 7.850 s | 171.489 s | CLI/HTTP workflow and real local inference passed |
| Packaged SIMD WASI worker | 8.043 s | 87.220 s | Same 11 searchable / 1 unreadable pages, original SHA and real local answer |

These are functional gates, not controlled throughput comparisons. Evidence is in
[`results/pdf-mvp`](results/pdf-mvp), under the corresponding `product-*-01`
directories. The original small PDF is 1,598,418 bytes, SHA-256
`504a6a9fa2b76f3c826a50c767c9c0abf7bc498a7537d2760f909b8f73f55dfe`.

The native Wails/WebView2 window also passed import, search while 11 pages were
pending, a real Qwen answer giving 15 March, and opening the original at physical
page 11. See `product-native-proxy-01/native-ui.png`, `native-ui.json`,
`native-partial-search.json` and `network-control.json`. First useful native search
was 8.037 s. The WebView2 toolbar displayed page 11 of 12.

For native offline testing, a test-only Go overlay enabled CDP and directed
external browser traffic to a dead loopback proxy. Its negative control recorded
`net::ERR_PROXY_CONNECTION_FAILED` for example.com; local UI, Bacalhau and inference
remained reachable. Child process HTTP proxies used the same dead endpoint and
PATH contained only Windows directories. No global proxy/firewall settings changed.
The production binary has no CDP/proxy test options. A separate restricted-token
CLI/HTTP run passed; WebView2 crashed under that token, so that attempt is not
claimed as a successful native GUI test. Failed attempt logs remain available.

## Build and use

Build the WASI worker using the commands in [the technical journal](pdf-mvp.md),
then package it with the Portuguese OCR model and pinned Bacalhau:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/package-pdf.ps1
bin/pdf-mvp/VIII.exe
```

`runtime/` must stay beside VIII.exe. The package includes OCR assets and verifies
their hashes before execution. Default data storage is `%APPDATA%/VIII`; a single
PDF is active at a time. Existing confirmed page files are immutable. Importing
the currently completed original again keeps its processing state. The original
is checked before each job and before serving it through the PDF viewer.

Questions require a separately installed local llama.cpp server and a GGUF model:

```powershell
llama-server.exe -m qwen.gguf --host 127.0.0.1 --port 48081 --alias local --ctx-size 4096 --threads 4 --parallel 1 --n-gpu-layers 0
```

The validated model/runtime identities are in
`results/pdf-mvp/answer-proof/source.json`. Only a loopback chat-completions endpoint
is accepted; searching and OCR do not require inference. Missing passages and a
partial index are explicitly reported, and citations come from retrieved pages.

For acceleration, start Docker Desktop with Linux containers and build the worker:

```powershell
docker build -f scripts/pdf-docker-worker.Dockerfile -t viii-pdf-worker:local .
```

The app starts a pinned Bacalhau compute container and stages the local original
in its own Docker volume. Bacalhau remains the sole job orchestrator. Selecting
Docker first resolves any active WASI execution; an unavailable Docker installation
leaves WASI enabled. The default offline package does not include Docker images.

## Repeat functional and large tests

```powershell
go test ./... -count=1
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/validate-pdf-product.ps1 -Package bin/pdf-mvp -Mode wasi -Results .poc-runtime/pdf/repeat-wasi
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/validate-pdf-product.ps1 -Package bin/pdf-mvp -Mode docker -Results .poc-runtime/pdf/repeat-docker
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/validate-pdf-product.ps1 -Package bin/pdf-mvp -Mode switch -Results .poc-runtime/pdf/repeat-switch

$env:PYTHONPATH="$PWD/.poc-runtime/pdf/python-deps"
python scripts/make-pdf-fixture.py .poc-runtime/pdf/fixtures/mixed-30000.pdf --pages 30000 --ocr-every 10
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/benchmark-pdf.ps1 -Engine wasi -Results .poc-runtime/pdf/benchmark-wasi-30000
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/benchmark-pdf.ps1 -Engine docker -Results .poc-runtime/pdf/benchmark-docker-30000
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/benchmark-pdf-concurrency.ps1 -UIData .poc-runtime/pdf/repeat-wasi/data
```

Use fresh result directories. The benchmark saves identity, app ownership, machine
configuration, progress, latency, RAM and disk samples. After an interrupted
monitor, repeat its exact command with `-Resume`; do not manually start another
app against the same data directory. Processes stay running on monitor failure.
Sampled peaks are lower bounds. Windows RAM excludes the Docker VM; Docker stats
and volume samples are recorded separately. Permanent originals/pages are reported
separately from Windows temporary paths, and Docker volume size includes its staged
original. Model inference is excluded from processing measurements.

The concurrency comparison processes physical pages 29,961–30,000 of that same
full-size original, partitioned into 1, 2 or 4 disjoint jobs. Its 40-page range
contains five scans; each job incurs the actual original-file memory cost.

The 30,000-page fixture is 1,250,617,698 bytes, with 3,001 scanned pages (10.0033%).
SHA-256: `eb3ca405ce4b3601b2a06903283fecae52c19eda59521898ae86a70753ebb699`.
The late scanned target is physical page 29,999. A fresh full WASI SIMD run is
active under `.poc-runtime/pdf/benchmark-wasi-simd-30000-02`, using the validated
`bin/pdf-mvp-simd-io` package and the corrected collector. Docker and the 1/2/4
comparison follow sequentially with that same package. No completed large run
or concurrency result is claimed yet. Hayro currently owns the compressed PDF
byte buffer; the full run must establish its memory and throughput limits.

The earlier scalar run was stopped after 27,478 accepted pages, including 479 OCR
pages. Its [terminal cancellation record](results/pdf-mvp/scalar-partial-superseded.json)
confirms the job/execution resolved before the owned processes were stopped.
Its accepted files, original and logs remain intact; its sampled 8.69 GB working
set peak is a **partial scalar-run** result, not the final SIMD result.

SIMD128 was adopted after two real, reverse-order paired Bacalhau Windows probes
of the same dense scanned page: 52.870 → 22.627 s and 67.296 → 22.500 s, with
identical OCR text, physical reference and Portuguese model. See
[calibration and small product gate](results/pdf-mvp/simd-adoption.json). These
probes overlapped the existing scalar workstation run and are calibration data,
not isolated machine-wide throughput estimates. Only compiler vectorization
changed; default concurrency remains one. The full 30,000-page workload is being
run again from import with the validated variant, rather than treating a small
probe as large-document acceptance.

The first WASI timestamp of 113.763 s is **invalid**: Windows PowerShell wrapped
an empty JSON response array as one item, and that event has no physical page.
The [correction record](results/pdf-mvp/benchmark-first-query-correction.json) and
[real empty/nonempty-response regression](results/pdf-mvp/collector-array-regression.log)
preserve the diagnosis. The collector now expands the returned array and requires
a real page/citation. Both new full runs use that corrected collector; no separate
prefix trial is needed. Historical raw events remain intact. The first SIMD
monitor attempt ended before import because hidden Windows PowerShell failed to
autoload `Get-FileHash`. Explicitly loading its standard module passed three
hidden-process checks; attempt `-02` started afterward in a fresh directory.

That full SIMD run directly recorded a real first hit on physical page 1 while
29,999 pages were pending: **55.168 s from import**, or 58.611 s including monitor
startup. See [the actual query references](results/pdf-mvp/simd-first-useful-search.json).

At extraction range 1,154–1,281, the worker returned generic `I/O error` before
publishing metadata/pages. The app paused with 1,038 searchable pages and 1,153
digital observations preserved. Available physical memory was about 0.8 GB.
The exact failing operation/native errno was not logged, so the root cause is
unproven. Large Windows read buffers can encounter memory/page-locking errors
([ReadFile documentation](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-readfile));
that is a working hypothesis, not a confirmed diagnosis of this event.

The worker now requests at most 64 KiB per host read, rejects incomplete/changed
input lengths and records operation/error details. Hayro still holds the full
compressed PDF buffer; this does not claim a streaming PDF parser. The owned
Bacalhau process now uses a **soft** Go heap target of `3GiB`, reducing retained
heaps between ranges. The same target applies to each 1/2/4-job comparison.
Rust byte-integrity/bounded-read/range regressions, Go regressions and the rebuilt
small real-model product gate passed. The original full document was reopened
and resumed at physical page **1,154**; all **2,191** accepted/observed artifact
hashes remained unchanged. See [diagnosis](results/pdf-mvp/simd-io-diagnosis.json)
and [actual restart/resume](results/pdf-mvp/simd-io-resumed.json).

The full-run wall clock and prior peaks remain intact, including the diagnosis
pause and runtime configuration change. Final performance reporting must retain
that history rather than label the run as uninterrupted or uniformly configured.
The resumed run has passed further ranges; full-document success remains pending.

The user paused the run on 17 November after 27,003 searchable pages (four with
OCR); the current job and owned processes were stopped with results preserved.
On 18 November the repository/worktree had moved to a new parent directory.
`git worktree repair` restored the branch links. The runtime now preserves its
saved local node ID across profile moves (covered by regression tests). The old
API/NATS ports had become excluded Windows ports, so this stopped profile was
assigned free loopback ports without changing system reservations. Both the
source fixture and imported original SHA were rechecked. The same document
resumed at missing OCR page 50. See
[resume evidence](results/pdf-mvp/user-resume-relocated-20261118.json).

The collector preserves raw wall time and additionally reports active elapsed
and OCR durations excluding the recorded explicit user pause. Diagnostic pauses
remain included. The current package is `bin/pdf-mvp-resume`; Docker image caches
were absent after the pause, so the sequential queue will prepare those images
after WASI ends and before timing the Docker run.

The WASI monitor stopped once on a transient HTTP 500. Its app and Bacalhau process
remained alive and processing; only telemetry was resumed, with the gap retained
in the event log and the original wall clock preserved. The collector now retries
up to five transient errors. The source also serializes state-file readers with
replacement and retries brief Windows rename failures caused by open readers.
The Windows held-reader regression and metadata-before-ACK regression passed.
Recovery also restores a canonical page JSON that survived a crash before its
SQLite insertion, skips that physical page in subsequent work, and preserves its
original provenance. The regression includes an interrupted temporary file and
rejects a mismatched physical-page identity.
Before replaying a persisted submission without a known Bacalhau job ID, the
app rechecks the placement grant and the original/worker hashes. The regression
verifies that changing either file or removing the grant prevents resubmission.
The SIMD package includes these recovery changes and passed the full small
product gate. Its SHA is recorded in the new benchmark's machine metadata. Final
large-file/native-UI checks and the acceptance audit remain pending. Development
and ordinary workstation activity occurred during the preliminary scalar run;
its experiment annotation retains that scope.

The user paused again on 18 November at 27,112 searchable pages (113 OCR).
The job reached `Stopped` with execution state 11 before its owned runtime was
closed. On explicit `Continue`, both PDF hashes were verified and the same
document resumed at physical page 1,140, preserving the searchable count. This
second pause is separately added to the excluded user-pause duration. See
[resume evidence](results/pdf-mvp/user-resume-20261118-134135.json) and
[current package hashes](results/pdf-mvp/package-identity-continue-20261118.json).
The initial machine/package record remains historical; the rebuilt desktop
preserves node identity after the move, with unchanged bounded-I/O worker,
Portuguese OCR model and pinned Bacalhau runtime.
