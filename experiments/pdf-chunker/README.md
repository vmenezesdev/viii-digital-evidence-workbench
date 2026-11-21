# Experiment: bounded-memory PDF chunking for VIII

**Branch base:** `mvp/pdf-digestor`, original head `17a23728c677a131aedb1ec4a34b61baa7545702`.
Isolated from PR #12; it must not modify the MVP runtime or its OCR experiment data.
**Current state and decision: [large-PDF chunking overview](../../docs/poc/pdf/chunking.md)
and [ADR-0003](../../docs/adr/0003-chunk-large-pdfs-with-flpdf-wasi.md) (flpdf batch, Accepted;
not integrated into VIII).** The reports below are the detailed record, in order:

1. [RESULTS.md](reports/RESULTS.md): pdfcpu, pdq, qpdf; Bacalhau WASI behaviour; OCR on chunks (its pdq recommendation is superseded).
2. [ALTERNATIVES.md](reports/ALTERNATIVES.md): on-demand I/O, flpdf and otf-pdf.
3. [BATCH-RESULTS.md](reports/BATCH-RESULTS.md): batch pdq vs flpdf, `lean` route, adaptive selection.

## Question

Can a separate chunking stage produce a small, valid derivative PDF for an
arbitrary physical page range of a 30,000-page original, so that the existing
Bacalhau + WASI Hayro/Tesseract worker (which reads the whole PDF into linear
memory) only loads the pages it processes, without losing portability,
reliability or provenance?

## Compatibility levels (never conflated)

1. compiles to WebAssembly;
2. compiles and runs under a WASI Preview 1 runtime (Wasmtime);
3. runs correctly as a job on the **pinned VIII Bacalhau v1.9.5** (revision
   `3a6a73aa…`, wazero v1.12.0) on Windows, Docker disabled, network `none`,
   read-only mounts, ready/acknowledgement protocol, verified outputs.

Only level 3 counts as compatibility. CI covers levels 1-2; level 3 is local.

## Contents

| Path | Purpose |
|---|---|
| `reports/` | The three measurement reports, in order: `RESULTS.md`, `ALTERNATIVES.md`, `BATCH-RESULTS.md`. |
| `results/` | Committed structured results each report is built from (summaries only, no PDFs). |
| `harness/` | Python runners, validators and their tests: `bench.py` (native, exact OS peaks), `bacalhau_wasi.py` (real jobs on an isolated pinned node), `validate.py` (Poppler oracle), `e2e_ocr.py` (chunk → OCR → physical citation), `run_matrix.py`, `summarize.py`, `adversarial.py`, `alt_*.py` (ALTERNATIVES.md), `batch_*.py` and `selection_model.py` (BATCH-RESULTS.md), `test_bench.py`, `test_batch.py`. |
| `workers/flpdf-wasi/` | flpdf 0.10.0 chunker as measured; the supported copy is `workers/pdf-chunker` at the repository root (ADR-0003). |
| `workers/pdq-wasi/` | pdq 0.5.1 plus `wasi-read-instead-of-mmap.patch`; `prepare.py` fetches the crate, verifies its sha256 and applies the patch. |
| `workers/pdfcpu-go/` | Go `wasip1` chunker using pdfcpu v0.16.4 (`api.Trim`), with tests. |
| `workers/otfpdf-wasi/` | otf-pdf 0.2.0 chunker; blocked by a classic-xref rescan. |
| `workers/qpdf-wasi-probe/` | Minimal C++ exception probe: the qpdf port gate. |
| `workers/fsprobe/` | Characterises the Bacalhau WASI file system seen by a guest. |

## Worker contract (both chunkers)

```
chunker original.pdf first last out-dir ack-file [declared-source-sha256]
```

Writes `out-dir/chunk.pdf`, `manifest.json` (schema `viii.pdf-chunk.v1`:
tool/version, physical range, exact `local -> physical` map, chunk sha256,
page count verified by re-reading the derivative) and finally `ready` with the
content `ready`; then waits for the host to create `ack-file` and exits 0.
The derivative is an **operational artifact, not evidence**: citations must
use the manifest's physical pages of the original, whose sha256 is checked
before and after every job.

Bacalhau v1.9.5's WASI result mount (`/out`) on Windows behaves unlike a
normal file system (observed with `fsprobe`): `stat`/`open` of a missing path
**creates an empty file**, `O_EXCL` is ignored, `rename` and `remove` are not
implemented, `mkdir` of the mount root returns ENOSYS. Workers therefore never
stat inside `/out`, list it instead, write final names directly and write
`ready` last; hosts accept only `ready == "ready"` plus a hash-verified
manifest. Job stdout/stderr captured in `RunOutput` drops writes after the
first, so errors are emitted with a single write. A module that returns from
`_start` without `proc_exit` is reported with exit code -1.

## Reproduction (Windows PoC machine)

Use a separate worktree and fresh result directories, synthetic fixtures only,
and never run large tests while the MVP OCR benchmark is running (check for
`bacalhau.exe`/`VIII.exe` processes and `runner-status.json` first).

Tools used for the recorded results (place on `PATH`): Python 3.15 with
`psutil 7.1.3` and Pillow; Go 1.27.4; Rust with `wasm32-wasip1`; qpdf 12.4.2
(msvc64 zip, sha256 verified against the release `.sha256`); pdfcpu CLI
`go install github.com/pdfcpu/pdfcpu/cmd/pdfcpu@v0.16.4`; pdq CLI
`cargo install pdq --version 0.5.1 --locked`; Poppler 25.07 (`pdftotext`,
`pdftoppm`); Wasmtime 51.0.0; wasi-sdk 34 (probe only); the pinned
`C:\Bacalhau\bin\bacalhau.exe`.

```powershell
$R='.poc-runtime/pdf-chunker'
python scripts/make-pdf-fixture.py $R/fixtures/s12.pdf --pages 12 --ocr-every 4
python scripts/make-pdf-fixture.py $R/fixtures/s1000.pdf --pages 1000 --ocr-every 10
python scripts/make-pdf-fixture.py $R/fixtures/s10000.pdf --pages 10000 --ocr-every 10
# 30k: reuse the MVP fixture read-only after checking its sha256 (eb3ca405...)

# Build the WASI chunkers
Push-Location experiments/pdf-chunker/workers/pdfcpu-go; go test ./...
$env:GOOS='wasip1'; $env:GOARCH='wasm'; go build -trimpath -o ../../../../$R/pdfcpu-chunker.wasm .
Remove-Item Env:GOOS,Env:GOARCH; Pop-Location
Push-Location experiments/pdf-chunker/workers/pdq-wasi; python prepare.py; cargo test --release --locked
cargo build --release --locked --target wasm32-wasip1; Pop-Location
Copy-Item experiments/pdf-chunker/workers/pdq-wasi/target/wasm32-wasip1/release/viii-pdq-chunker.wasm $R/pdq-chunker.wasm

# Native matrix, WASI matrix, startup control, concurrency
python experiments/pdf-chunker/harness/run_matrix.py native --fixture s12=$R/fixtures/s12.pdf s1000=$R/fixtures/s1000.pdf s10000=$R/fixtures/s10000.pdf --out $R/matrix --repeat 3 --validate
python experiments/pdf-chunker/harness/run_matrix.py wasi --fixture s12=$R/fixtures/s12.pdf s1000=$R/fixtures/s1000.pdf s10000=$R/fixtures/s10000.pdf --module pdq=$R/pdq-chunker.wasm pdfcpu=$R/pdfcpu-chunker.wasm --out $R/matrix --repeat 2
python experiments/pdf-chunker/harness/run_matrix.py startup --module pdq=$R/pdq-chunker.wasm pdfcpu=$R/pdfcpu-chunker.wasm --out $R/matrix --repeat 3
python experiments/pdf-chunker/harness/run_matrix.py concurrency --fixture s10000=$R/fixtures/s10000.pdf --module pdq=$R/pdq-chunker.wasm --jobs 1 2 4 --out $R/matrix

# End-to-end provenance gate (unchanged MVP worker and model, hashes checked)
python experiments/pdf-chunker/harness/e2e_ocr.py --original $R/fixtures/s12.pdf --first 9 --last 12 `
  --chunker $R/pdq-chunker.wasm --worker <bundle>/runtime/pdf-worker.wasm --model-dir <bundle>/runtime/models `
  --expect-page 11 --baseline --results $R/runs/e2e-pdq-s12
```

The 30,000-page commands, and why some could not be completed on the PoC
machine, are in RESULTS.md.
