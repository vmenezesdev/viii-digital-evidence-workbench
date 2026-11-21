# PDF MVP: validation journal

Work in progress. The real OCR technical gate passed; the complete PDF acceptance
criteria are not yet achieved.
Only synthetic evidence is used. The original scope is in [MVP-PDF.md](MVP-PDF.md).

## Pinned runtime and first gate

Windows executable: `C:\Bacalhau\bin\bacalhau.exe`, module v1.9.5,
revision `3a6a73aa6529755ac2c9d96a771f0566ff3ee5da` (verified with `go version -m`).
Docker Desktop 4.42.1 / engine 28.2.2 is available for **building** dependencies;
the default processing proof explicitly disables Docker in Bacalhau.

Build and run the real Portuguese OCR proof from PowerShell at the repository root:

```powershell
docker build --progress plain -f scripts/pdf-worker.Dockerfile --output type=local,dest=.poc-runtime/pdf/artifacts .
New-Item -ItemType Directory -Force .poc-runtime/pdf/models
curl.exe -fL https://raw.githubusercontent.com/tesseract-ocr/tessdata_fast/4.1.0/por.traineddata -o .poc-runtime/pdf/models/por.traineddata
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/prove-pdf-ocr.ps1
```

The proof uses Tesseract 5.5.0, Leptonica 1.85.0 and wasi-sdk 25. It creates a
Portuguese synthetic BMP, passes local file references (no evidence bytes in job
arguments), pins the Windows node and records a JSON page artifact separately
from stdout. It checks `JABUTICABA`, physical page 1, unchanged input hash, exit
code and placement. This script is an early technical gate, not the complete PDF
workflow. A repeated run needs a fresh `-Results` directory.

## Failed attempts and diagnoses

Full build logs are in [results/mvp/builds](results/mvp/builds).

1. `build-attempt-1.log`: Leptonica built successfully. CMake 3.25 aborted in
   Tesseract's cross-compilation `try_run` of the TIFF probe. Supplying the
   cross-compilation result (`LEPT_TIFF_RESULT`) lets configuration finish.
2. `build-attempt-2.log`: compilation reached the LSTM implementation, where
   libc++ has no `std::mutex` in the single threaded WASI sysroot. OpenMP is off;
   adapt locks and thread launch to serial execution only in this WASI build.
3. `build-attempt-3.log`: serial thread shim lacked a default constructor and
   `joinable()`. Added the two required operations.
4. `build-attempt-4.log`: `GRAPHICS_DISABLED` does not remove the viewer sources
   from Tesseract 5.5.0's CMake source glob. `svutil.cpp` includes `netdb.h`, absent
   from WASI. Exclude viewer sources and build only `libtesseract`; the unused CLI
   target also cannot be disabled by `BUILD_TESSERACT_BINARY` in this version.
5. `build-attempt-5.log`: the libraries compiled, but linking exposed optional
   Leptonica `system`/`mkstemp` helpers. The WASI build returns `ENOSYS` for these
   unsupported operations; it never launches a native process.
6. `build-attempt-6.log` onward: successful WASI builds. The final proof module is
   6.12 MB. The subsequent rebuilds add acknowledgement and an explicit WASI exit.

Runtime attempts are preserved as `ocr-attempt-*.log`. They exposed a PowerShell
alias conflict, Windows result path validation (`/out` is not host-absolute), the
mandatory publisher for result paths, local allowlist separator sensitivity,
and the difference between mounting a file and mounting its containing directory.
An ordinary C `_start` return yields exit **-1** in this Bacalhau, so success uses
an explicit `proc_exit(0)` via `_Exit`.

The [successful OCR evidence](results/mvp/ocr/ocr-proof/summary.json) identifies
job `j-eabda74b-b257-4616-86a8-c5519cd9796c` and execution
`e-bfc7d7b2-eafc-4231-8456-cef1dabb6159` on `viii-pdf-wasi`. The job took
3.682 seconds including compilation, OCR and acknowledgement. Tesseract correctly
recognized `obrigação`, `Página física 1` and `JABUTICABA`. This is one small
synthetic image, not a throughput benchmark. `ocr-attempt-8.log` also proves that
`TestPDFOCRArtifact` persisted this real artifact through VIII's atomic writer.

Source inspection of the pinned Bacalhau established two relevant constraints:

- WASI input mounts use `os.DirFS` / `filefs`; output uses `touchfs` under the
  execution's `results/<ResultPath.Name>` directory. A `local` **file** input is
  mounted at its target exactly; a downloaded file has different staging semantics.
- The `local` publisher HTTP server binds **all network interfaces**, regardless
  of its advertised host. Keep it disabled. The one-workstation implementation
  ingests output files from the owned compute data directory, using the `noop`
  publisher. **Any publisher deletes execution results immediately after
  publishing**. Therefore the worker waits for a host acknowledgement after file
  ingestion before exiting; there is no race against post-exit polling. The
  unacknowledged execution directory otherwise has a one-hour cleanup delay.
  On reopening, missing/unconfirmed results still require reconciliation/retry.
  No external artifact server is needed.

## Remaining acceptance work

- Implement import authorization/hash, shared page result format, incremental
  progress, recovery and engine switching, UI and opening page citations. Page
  persistence, FTS5 and the single inference adapter exist but are not yet wired
  into the product UI/runtime.
- Bundle runtime/worker/model and verify offline first use without Docker.
- Validate the small mixed PDF through the product on both engines, including
  interruption/switch regressions and clicking a real-model answer's citation.
- Only then run both 30,000-page paths and the 1/2/4-job comparison, persisting
  throughput, first useful search, elapsed time, peak RAM/temp disk and responsiveness.

## PDF workers and small-fixture evidence

The worker is in `workers/pdf`, with Hayro 0.7.1 and a committed Cargo lockfile.
Its WASI build links the previously proven Tesseract/Leptonica libraries. The
native Docker build uses the same Rust code and Debian's Tesseract 5.3.0; tool
versions come from the OCR engine, not from a configured label. Both emit the same
page JSON contract. Digital-only mode publishes text without waiting for OCR.
OCR mode renders only pages with insufficient text, one raster at a time.

The synthetic fixture has 12 pages, four image-only pages, a blank page 8, and
`JABUTICABA` with a payment date on scanned page **11**. It is 1,598,418 bytes;
the [manifest](results/mvp/fixtures/fixture-small.json) records its SHA-256. The
[render of page 11](results/mvp/pages/pages-render-proof/page-000011.png) was
visually inspected for legibility and physical-page identity.

| Technical probe | Elapsed | Observed peak Windows node working set | Evidence |
| --- | ---: | ---: | --- |
| WASI extraction + rendering of 12 pages | 17.971 s | 176.6 MiB | [summary](results/mvp/pages/pages-render-proof/summary.json) |
| WASI extraction + Portuguese OCR, all 12 pages | 160.602 s | 358.7 MiB | [summary](results/mvp/pages/pages-wasi-proof/summary.json) |
| Docker extraction + Portuguese OCR, same PDF | 13.110 s | Not sampled | [summary](results/mvp/pages/pages-docker-proof/summary.json) |
| Current WASI module, only physical page 11, job networking disabled | 72.800 s | 296.1 MiB | [summary](results/mvp/pages/pages-selected-proof/summary.json) |

The isolated-page probe overlapped a Rust/Docker build. These are functional
probes, **not** a controlled throughput comparison. `first_artifact_ms` measures
PDF metadata availability, not the first useful search. Reported disk bytes count
worker output files, not total temporary storage. No machine-wide RAM or desktop
responsiveness claim follows from these numbers.

Both full-worker probes recognized the term/date on page 11 and marked blank
page 8 `unreadable`. Opt-in Go tests ingested the actual output into VIII's atomic
page store and SQLite FTS5, found the scanned term, and returned a citation URL
ending in `#page=11`. This does **not** yet prove clicking/opening that URL in the
product: its HTTP/UI route remains to be implemented.

Regression coverage now includes immutable accepted pages across reopen/engine
metadata changes, literal FTS queries, accent-insensitive search, partial-index
retrieval, missing-evidence behavior, local inference endpoint restriction and
citations derived from retrieved references. These tests do not substitute for
live interruption/cancellation/switch tests. `go test ./... -count=1` passes;
the Docker build also executes the Rust physical-page-range regression.

### Reproduction

```powershell
$env:PYTHONPATH = "$PWD/.poc-runtime/pdf/python-deps"
python scripts/make-pdf-fixture.py .poc-runtime/pdf/fixtures/small.pdf
docker build -f scripts/pdf-worker.Dockerfile --output type=local,dest=.poc-runtime/pdf/artifacts .
$env:VIII_PDF_WASI_LIBS = "$PWD/.poc-runtime/pdf/artifacts/lib"
$env:CARGO_TARGET_DIR = "$PWD/.poc-runtime/pdf/rust-target-simd"
$env:RUSTFLAGS = '-C target-feature=+simd128'
cargo build --manifest-path workers/pdf/Cargo.toml --release --target wasm32-wasip1 --locked
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/prove-pdf-pages.ps1 -Mode ocr
docker build -f scripts/pdf-docker-worker.Dockerfile -t viii-pdf-worker:local .
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/prove-pdf-docker.ps1
$env:VIII_PDF_PAGES_PROOF = "$PWD/.poc-runtime/pdf/pages-proof"
go test ./cmd/desktop -run '^TestPDFMixedArtifacts$' -count=1 -v
```

Dependencies for the fixture: ReportLab and Pillow, installed only under
`.poc-runtime/pdf/python-deps`. `rustup target add wasm32-wasip1` supplies Rust's
WASI standard library. The Windows build used Rust 1.99.1. Choose fresh output
directories (`-Results`) for each probe; fixture generation also refuses to
overwrite its output. Docker proof resources are owned/named by the script and
cleaned up on exit; `owned.json`, node logs, job description and artifacts remain
in its results directory.

### Limits to address before acceptance

Hayro currently reads the compressed PDF bytes into memory, while releasing each
page's decoded raster and render cache before processing the next page. The small
fixture demonstrates page-at-a-time rendering, not bounded memory for arbitrary
large PDFs. The 30,000-page runs must establish whether this meets the memory
requirement; a failure near the WASI address-space limit requires a streaming
reader alternative rather than silently claiming large-document support.

The inference adapter uses only **llama.cpp's local chat-completions endpoint**.
The real-model gate passed with llama.cpp Windows CPU `b12453` and
Qwen2.5-0.5B-Instruct Q4_K_M (491,400,032 bytes), running on loopback port 48081.
The [answer](results/mvp/product-gates/answer-proof/answer.json),
[identity record](results/mvp/product-gates/answer-proof/summary.json), pinned public model
revision and [test log](results/mvp/product-gates/answer-attempt-1.log) are retained.
`TestPDFRealLocalAnswer` took 1.28 seconds and returned the correct payment date,
with retrieved physical-page reference 11 and an explicit partial-index flag.
This verifies the inference adapter over real worker artifacts; the product UI
and PDF-open route remain outstanding. The temporary inference server was stopped.

Repeat this gate after downloading the CPU runtime/model identified in
`results/mvp/product-gates/answer-proof/source.json`:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/prove-pdf-answer.ps1
```

For product integration, a running local `llama-server` and a GGUF model are a
separate prerequisite. The proof script records its loopback-only launch command,
owns/stops its process and does not require Ollama or any cloud API. The model
and binary stay under the ignored `.poc-runtime/pdf/inference` directory.

The additional [Docker control](results/mvp/pages/pages-docker-offline-job/summary.json)
completed in 12.838 seconds with task networking disabled and both the Windows
original and the mounted/staged copy's hashes checked. A job's disabled networking
does not establish offline first use of an installed VIII package.

The small **product** workflow, native UI, real inference, live restart/engine
switching and first-use offline gates have since passed. The current product
results, packaging and reproduction commands are in [product.md](product.md).
Earlier statements above describing these routes as outstanding refer to the
technical-probe stage. The 30,000-page measurements remain pending.
