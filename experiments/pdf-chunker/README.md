# Experiment: bounded-memory PDF chunking for VIII

**Branch base:** `mvp/pdf-digestor`, original head `17a23728c677a131aedb1ec4a34b61baa7545702`.
This experiment is separate from PR #12 and must not modify the current long-running OCR data directory.

## Decision to test

Find a chunker which produces a **small, structurally valid derivative PDF** for an
arbitrary 1-based physical page interval of a 30,000-page original, without
resident memory growing approximately with the original PDF size.
This is a candidate preprocessing stage for the existing Bacalhau + WASI
Hayro/Tesseract worker, **not** a replacement for evidence identity.

Three native baselines are supplied:

- **pdfcpu (Go)**: `trim --pages A-B`; a Go `wasip1/wasm` worker using `api.TrimFile` is included.
- **pdq (Rust)**: `split original.pdf --out A-B derivative.pdf`; mmap-based, WASI viability unverified.
- **qpdf (C++)**: `--empty --pages original.pdf A-B -- derivative.pdf`; native reference, WASI viability unverified.

No candidate is considered WASI-compatible with the pinned Bacalhau until its
actual job finishes with a verified output. An external WASI build is not sufficient.
A successful split does not establish low memory or semantically correct OCR.

## Reproduction on the Windows PoC machine

Use a separate checkout/worktree and **fresh results directories**.
Use only synthetic fixtures, and do not run the large test concurrently with the
current OCR benchmark because memory contention invalidates comparisons.

Requirements: Python 3, `psutil` for RSS, `qpdf` for independent structural
checks, candidate CLIs `pdfcpu` and `pdq`, Go >= 1.27.4 for the pinned
pdfcpu module, and the pinned VIII Bacalhau binary for the WASI proof.
Pin and record exact binary versions/hashes for any results promoted into an ADR.

```powershell
python -m pip install psutil
python scripts/make-pdf-fixture.py .poc-runtime/pdf-chunker/fixtures/small.pdf --pages 12 --ocr-every 4
python experiments/pdf-chunker/bench.py --input .poc-runtime/pdf-chunker/fixtures/small.pdf --first 9 --last 12 --results .poc-runtime/pdf-chunker/small-01
```

Review `summary.json` and per-command logs; do not infer a win from exit code alone.
The independent qpdf checks verify document structure and number of output pages,
not visual or text parity. The input SHA-256 is checked before and after.

After the small fixture passes, and **only when the currently running benchmark is
not competing for the machine**, compare the fixed original's late physical range:

```powershell
python experiments/pdf-chunker/bench.py --input .poc-runtime/pdf/fixtures/mixed-30000.pdf --first 29961 --last 30000 --results .poc-runtime/pdf-chunker/large-01
```

### pdfcpu Go/WASI integration proof

The module is isolated from the repo's root `go.mod`. The build command is:

```powershell
Push-Location experiments/pdf-chunker/pdfcpu-go
go mod download
$env:GOOS='wasip1'
$env:GOARCH='wasm'
go build -trimpath -o ../pdfcpu-chunker.wasm .
Remove-Item Env:GOOS,Env:GOARCH
Pop-Location

powershell -NoProfile -ExecutionPolicy Bypass -File experiments/pdf-chunker/prove-pdfcpu-wasi.ps1 `
  -PDF .poc-runtime/pdf-chunker/fixtures/small.pdf `
  -Worker experiments/pdf-chunker/pdfcpu-chunker.wasm `
  -First 9 -Last 12 `
  -Results .poc-runtime/pdf-chunker/wasi-small-01
```

The WASI worker writes `chunk.pdf`, `info.json` and a `ready` marker,
then waits for host acknowledgement before exit. The runner starts its **own**
loopback Bacalhau node, disables Docker, copies and verifies the output hash,
acknowledges and stops only the node it owns.

This runner is an *integration probe*, not a validated gate: Bacalhau's response,
Go's WASI output semantics and pdfcpu memory characteristics must be observed
on Windows, and the runner may need adjustments based on real diagnostics.
Avoid using the proof results as acceptance evidence until this happens.

## Further semantic and scaling gates

1. Original PDF SHA-256 unchanged; derivative SHA recorded and reproducible
   against a pinned chunker version.
2. qpdf structural validation succeeds; output has exactly `B-A+1` pages.
3. Existing `workers/pdf` can render and OCR the derivative. The page-local
   number `n` maps back to the original physical page `A+n-1`.
   Verify the scanned target at original page 29,999 with `JABUTICABA`,
   using the original PDF viewer, not a derivative citation.
4. Benchmark time, sampled process-tree RSS, output bytes and disk amplification
   at 12, 1,000, 10,000 and 30,000 pages with controlled corpus generation.
   Sampled RSS is a lower bound, not peak allocation. Record host/VM memory as well.
5. Compare work duplicated by four concurrent range jobs; repeated mmap and file
   cache bytes require a separate machine-wide metric.
6. Repeat on representative PDFs with shared resources, compressed object streams,
   incremental updates, encryption and malformed xref. Only synthetic/copyright-safe
   inputs should go into committed CI fixtures.
7. Check privacy: no network, no evidence contents in job arguments or log output.
   Keep the original immutable and reject derivations if its identity changes.

**Decision threshold (proposal):** both full-page correctness and actual Bacalhau
WASI execution must pass; 1.25 GB input should not require approximately 1.25 GB
per concurrent worker; the improvement should outweigh preprocessing time and
disk amplification. Otherwise retain the current implementation and consider
a native preprocessing backend or a lazy PDF parser. No winner is declared yet.

## Status

- [x] Isolated branch and candidate/harness sources.
- [ ] Dependency installation and local CLI smoke test.
- [ ] Native candidate comparison on small synthetic PDF.
- [ ] `GOOS=wasip1 GOARCH=wasm` build of pdfcpu.
- [ ] Actual Bacalhau Windows execution and semantic OCR gate.
- [ ] Controlled large-file and parallelism comparison.
- [ ] Evidence-backed recommendation and integration decision.
