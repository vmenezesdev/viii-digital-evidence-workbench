# PDF chunking for VIII: results and decision

> **Historical record (round 1 of 3).** Its recommendation, pdq-WASI as a batch job, is
> superseded by [ADR-0003](../../docs/adr/0003-chunk-large-pdfs-with-flpdf-wasi.md) (flpdf batch, after [ALTERNATIVES.md](ALTERNATIVES.md) and
> [BATCH-RESULTS.md](BATCH-RESULTS.md)). The measurements stand for what they measured.
> Current state: [large-PDF chunking overview](../../docs/poc/pdf-chunking.md).

Experiment branch `experiment/pdf-chunker-wasi` (PR #13, base `mvp/pdf-digestor`), issue #11.
Measured on 2026-11-20 on the Windows PoC workstation. Every number below
comes from the structured files in [`results/`](results/), produced by the
scripts in this directory from raw runs. Units: MiB/GiB (2^20/2^30 bytes).
Labels: **[F]** observed fact, **[I]** inference from observations,
**[H]** hypothesis not yet demonstrated.

## Answer

**Qualified outcome A.** A pdq-based WASI chunker, run as a *batch
preparation job* (one pass per document), is the only candidate that passed
every gate on the pinned Bacalhau v1.9.5 (Windows, Docker disabled, network
`none`, read-only original). It makes the memory of every OCR job independent
of the document size, with identical OCR output and correct physical-page
citations. It does **not** make the preparation step itself bounded: that job
transiently needs ~2.3x the PDF size inside Bacalhau. Recommended as an
incremental, flag-guarded integration; details and open risks in
[Decision](#decision).

Decisive test **[F]**: chunking physical pages 29,961-30,000 of the 30,000-page
synthetic original (sha256 `eb3ca405...`) in WASI, then OCR of the derivative
with the unchanged MVP worker, found `JABUTICABA` on chunk-local page 39 and
cited **physical page 29,999 of the original**. OCR text and source were
identical to OCR of the original range for 40/40 pages. The original's sha256
was unchanged. ([`results/e2e.json`](results/e2e.json))

## Environment and method

- Windows 11 Home 10.0.26300, 12 logical / 8 physical cores, 15.7 GiB RAM.
  **Shared workstation**: desktop applications held 11-13 GiB, so available RAM
  varied between 2.0 and 5.6 GiB during the runs. No MVP OCR experiment was
  running: `benchmark-wasi-simd-30000-02` was paused and no `bacalhau.exe` or
  `VIII.exe` process existed. Its data, fixtures and bundles were only read.
- Bacalhau v1.9.5, revision `3a6a73aa...`, wazero v1.12.0, sha256 `6ee846f8...`.
  Each WASI run uses its own fresh node on free loopback ports, with Docker
  disabled, `Network: none`, read-only `local` inputs, a `noop` publisher and
  ready/acknowledgement. Full identities are in [`results/environment.json`](results/environment.json).
- Candidates: pdfcpu v0.16.4 (Go, Apache-2.0); pdq 0.5.1 (Rust,
  Artistic-2.0, built on lopdf 0.43 MIT); qpdf 12.4.2 (C++, Apache-2.0).
  Independent oracles: qpdf `--check`/`--show-npages` and Poppler 25.07
  `pdftotext`/`pdftoppm`.
- Memory **[F]**: native runs report the child's exact OS peak working set and
  peak private bytes, read from its handle after exit. WASI runs report the
  node process's lifetime peak (read from its handle after exit) minus its
  idle RSS, measured before any job was submitted. The module runs inside the
  node (wazero), so this figure includes wazero's compile and runtime
  overhead. A 200 ms sampled timeline is a cross-check only. `read_bytes`
  counts bytes requested from the OS, file-cache hits included. pdq's mmap
  reads count as 0.
- Fixtures: deterministic synthetic PDFs from `scripts/make-pdf-fixture.py`.
  s12 (1.2 MiB), s1000 (40 MiB), s10000 (398 MiB) and the MVP's
  `mixed-30000.pdf` (1,193 MiB, 30,000 pages, 10% scanned, `JABUTICABA` on
  scanned page 29,999). The sha256 of each matched its manifest before and
  after every run.
- Ranges per document of N pages: `p1` = [N-1], `p32` = 32 pages in the
  middle, `late40` = [N-39, N]. Native runs: 3 repeats. WASI runs: 2 repeats
  (1 at 30k).

## Compatibility levels

| Candidate | 1. compiles to Wasm | 2. runs in WASI P1 (Wasmtime 51) | 3. runs on pinned Bacalhau |
|---|---|---|---|
| pdfcpu (Go) | **yes** | **yes**, after fixes (config dir, see below) | **yes**, 12 -> 30,000 pages |
| pdq (Rust) | **yes** (unmodified) | **no** unmodified (`memmap2`: `Unsupported`); **yes** with `wasi-read-instead-of-mmap.patch` | **yes** with patch, 12 -> 30,000 pages |
| qpdf (C++) | not built (effort limit) | probe: C++ exceptions (exnref) run in Wasmtime 51 | **no**: wazero v1.12.0 rejects both Wasm exception-handling encodings (`invalid value type: 105`; `invalid section id`). qpdf depends on C++ exceptions. v1.12.0 is also the newest wazero release. |

Level 3 also required working around Bacalhau v1.9.5 behaviour that Wasmtime
does not exhibit (next section). A Wasmtime pass alone would have been misleading.

## Bacalhau v1.9.5 WASI behaviour found on Windows [F]

Characterised with `fsprobe/` and the workers:

1. In the result mount `/out`, `stat`/`open` of a **missing path creates an empty
   file**. A worker that checks "does `ready` exist?" creates a fake `ready`.
2. `O_EXCL` is ignored. `rename` and `remove` are not implemented (ENOSYS), so
   the usual temp-file + rename atomic write fails. pdfcpu's `TrimFile` uses it.
3. `mkdir` of the `/out` mount root returns ENOSYS; Rust `create_dir_all` fails.
4. `RunOutput.Stdout/Stderr` keep only the first write of the module. Later
   writes are dropped with `Truncated=false`.
5. A module that returns from `_start` without `proc_exit` is reported with
   exit code **-1**.
6. The **job** is `Completed` even when the module exits 1. Only the execution
   exit code reveals the failure.
7. Failed executions are retried on the same node, so attempt history must be
   kept.
8. The result directory is removed after the execution ends. Outputs must be
   copied before the acknowledgement.
9. The same pdq module peaks at ~1.02x its input under Wasmtime but ~2.3x
   under Bacalhau/wazero (see memory table). The cause inside wazero is
   **[H]** (linear-memory growth by reallocation); it is not verified.

Mitigations (in both workers and `bacalhau_wasi.py`): never stat inside `/out`
and list it instead; write final names directly with `ready` (content
`"ready"`) last. The host accepts only `ready == "ready"` plus a manifest whose
chunk hash, range and exact local->physical map verify. Errors go out in a
single write, the worker exits explicitly, and the outcome is taken from the
execution exit code and state, never from the job state.

## Measurements

### Native extraction (exact OS peak; median of 3) [F]

| Document | Metric | pdfcpu | pdq | qpdf |
|---|---|---|---|---|
| s1000 (40 MiB) | peak working set | 68-69 MiB | 7.5-10.9 MiB | 11.0-11.4 MiB |
| | time | 0.13-0.14 s | 0.02 s | 0.07 s |
| s10000 (398 MiB) | peak working set | 536-537 MiB | 15-21 MiB | 38 MiB |
| | bytes read | 513 MiB | (mmap) | 7-10 MiB |
| | time | 0.77-0.86 s | 0.05-0.09 s | 0.47-0.49 s |
| s30000 (1,193 MiB) | peak working set | **1,602-1,672 MiB** | **29-46 MiB** | **99 MiB** |
| | peak private | 1,688-1,725 MiB | 14-15 MiB | 97 MiB |
| | bytes read | 1,539 MiB | (mmap) | 20-23 MiB |
| | time | 2.36-2.65 s | 0.14-0.23 s | 1.43-1.46 s |
| all | derivative byte-reproducible | **no** | yes | yes (with `--deterministic-id`) |
| all | late40 content vs original (text and 30-dpi raster) | identical | identical | identical |

pdfcpu's `Read` dereferences every object and loads all stream data, so its
memory and I/O grow with the file **[F]** (source plus the table above). pdq
uses mmap, an xref-only bootstrap, lazy objects and a page walk that stops at
the last requested page. qpdf reads objects on demand through a seekable input.

### WASI on the pinned Bacalhau (node peak minus idle baseline; median and range) [F]

| Document | Metric | pdfcpu | pdq (patched) |
|---|---|---|---|
| startup control (module exits at once) | time / memory | 10.1-11.4 s / 523-539 MiB | 0.61-0.76 s / 29 MiB |
| s12 | ready / memory | 10.2 s / 539 MiB | 0.69 s / 37 MiB |
| s1000 | ready / memory | 15.5 s / 556 MiB (max 880) | 0.80 s / 122 MiB |
| s10000 | ready / memory | 62.9 s / 2,241 MiB (max 2,750) | 1.92 s / 928 MiB |
| s30000 late40 | ready / memory | 176 s / 6,274 MiB, private 6,478 MiB* | 4.6-7.3 s / 2,719 MiB |
| module size | | 23.1 MB | 1.4 MB |

\* This run completed, but during it host available RAM fell to ~7 MiB for
~146 s because of a runner defect (see Incidents), so its timing is inflated
by paging. The same module under Wasmtime 51 **[F]**: s10000 418 MiB / 0.4-1.6 s,
s30000 **1,215 MiB / 1.0-4.2 s**, with the same chunk hashes
([`results/runtime-comparison.json`](results/runtime-comparison.json)).

pdfcpu's per-job startup alone (~10 s, ~0.5 GiB to compile a 23 MB Go module
in wazero) and its document-proportional memory, ~5.3x the file at 30k in
Bacalhau, rule it out **[F]**.

### Batch preparation and first usable chunk [F]

| | s10000 (1-10000, 40-page chunks) | s30000 (1-30000, 40-page chunks) |
|---|---|---|
| chunks / total size | 250 / 397 MiB (0.95x original) | 750 / 1,191 MiB (0.95x original) |
| WASI job until `ready` | 8.3 s | 26.2 s |
| node peak minus baseline | 928 MiB | 2,737 MiB |
| host copy + verification of every chunk hash, then ack | until 15.0 s | until 57.6 s |

First usable chunk: native pdq 0.14-0.23 s for one range at 30k; WASI pdq
4.6-7.3 s for one range or 26 s for the whole document in batch. The
in-place MVP has no preparation step.

### OCR on chunks vs on the original [F]

One `workers/pdf` OCR job on the 40 late pages of the 30,000-page PDF:

| | prepare | OCR job peak | OCR wall | prepare + OCR |
|---|---|---|---|---|
| chunk path | 4.6 s, 2,719 MiB transient | **270 MiB** | 129.5 s | 134.1 s |
| original (current MVP) | - | **2,890 MiB** | 142.0 s | 142.0 s |

k simultaneous OCR jobs on one node, 40 late pages each, s10000 (4-5
scanned pages per job):

| k | node peak, chunks | node peak, original | wall, chunks | wall, original | min host available, chunks / original |
|---|---|---|---|---|---|
| 1 | 307 MiB | 1,094 MiB | 142.7 s | 144.7 s | 4,236 / 3,155 MiB |
| 2 | 522 MiB | 2,153 MiB | 135.6 s | 133.6 s | 3,840 / 2,262 MiB |
| 4 | 1,018-1,042 MiB (2 runs) | **4,076 MiB** | 133.0-133.8 s | 132.4 s | 3,475-3,531 / **842 MiB** |

A first k=4 run on the original was aborted by the host memory guard at
4,130 MiB sampled, with 516 MiB available **[F]**. Throughput is the same on
both paths (OCR-bound, ~4x with 4 jobs). Only memory differs **[F]**.
Responsiveness was assessed through host available memory only. Interactive
latency was not measured.

For reference, the original MVP benchmark recorded a sampled VIII + node peak
of 5.94 GiB working set / 7.34 GiB private on its 30k WASI run
(`benchmark-wasi-simd-30000-02/state.json`, read-only). That is a different
method, and it is not a measurement from this experiment.

### Forensic correctness [F]

- Original sha256 unchanged before and after every native, WASI, batch, OCR,
  adversarial and concurrency run (`mounts_unchanged` / `input_unchanged`).
- Each chunk's manifest (`viii.pdf-chunk.v1` / `viii.pdf-chunk-set.v1`)
  records tool and version, the declared original sha256, the physical range,
  an exact local->physical map, the chunk sha256 and a page count obtained by
  re-reading the derivative. The host re-hashes the chunk before
  acknowledging. The run summary records the module and Bacalhau sha256, job
  and execution IDs, and every execution attempt.
- pdq derivatives are byte-reproducible across runs, across native mmap vs
  WASI, and across wazero vs Wasmtime. Late40 at 30k is always `5ade5f8b...`,
  so a chunk can be re-derived and checked independently. pdfcpu is not
  reproducible: object numbering follows Go map order, and timestamps and
  `/ID` vary. qpdf is reproducible only with `--deterministic-id`.
- No job stdout/stderr contained fixture text (leak scan for `JABUTICABA`,
  `Documento`, `prova documental`, `ARARA`).

### Adversarial cases (pages 2-3; Poppler as oracle) [F]

| Case | pdfcpu (native, WASI) | pdq (native, WASI) | qpdf (native) |
|---|---|---|---|
| shared `/Resources` holding every image | correct, 1.7 KiB | correct but **1,242 KiB**: the images of the whole document were copied into a 2-page digital chunk | correct, 1.7 KiB |
| object streams + xref stream | correct | correct | correct |
| incremental update (new revision of page 2) | correct (latest revision) | correct | correct |
| lying xref (all offsets +7) | correct, **silent repair** | correct, **silent repair** | correct, **silent repair** |
| truncated (no xref/trailer) | correct, warning | correct, warning | correct, exit 3 (warnings) |
| AES-256, owner password only | correct, **silently decrypted** | correct, **silently decrypted** | correct, **silently decrypted** |
| AES-256, user password | fails cleanly | fails cleanly | fails cleanly |

No candidate produced wrong content. Not supported and not to be relied on:
encrypted inputs. Their policy is undecided: a decrypted derivative of
protected evidence needs an explicit decision. Repairs are also unsupported
as a provenance signal: the WASI manifests do not yet record that the
original was damaged and reconstructed, and pdq's warnings are lost in
Bacalhau's truncated `RunOutput`.

## Decision

| Criterion | pdfcpu WASI | pdq WASI (patched) | qpdf | pdq native (B) |
|---|---|---|---|---|
| 1. Bacalhau 1.9.5 + WASI P1 | passes (level 3) | passes (level 3) | **blocked** (no Wasm EH) | outside Bacalhau |
| 2. memory, 30k | 6.3 GiB/job | 2.7 GiB once per document; OCR jobs 270 MiB | 99 MiB native only | 29-46 MiB |
| 3. arbitrary ranges | yes, ~10 s startup/job | yes, 4.6-7.3 s/range; batch 26 s | yes (native) | yes, 0.14-0.23 s |
| 4. fidelity incl. scanned | identical | identical; OCR identical 40/40 | identical | identical (same bytes as WASI) |
| 5. identity and provenance | non-reproducible bytes | reproducible bytes, manifest, physical citation | reproducible with flag | reproducible |
| 6. CPU/I/O/disk/startup | worst on every axis | +0.95x disk for a full batch | - | best |
| 7. maintenance | upstream API, ~0.5 GiB startup | local patch on a young crate (0.5.1) | needs wazero EH | small binary (1.4 MB); new non-Bacalhau execution path |

**Recommendation [I]:** adopt pdq-WASI as a *batch preparation job*. It is a
qualified outcome A: criteria 1 and 3-5 are met and criterion 2 is only
partially met. Integrate it incrementally:

1. Behind a flag, after import: one Bacalhau WASI job splits the original into
   fixed chunks (for example 40 pages) and writes a chunk-set manifest. The
   host verifies the hashes and stores the chunks as *operational
   derivatives* bound to the evidence sha256, never as evidence.
2. OCR jobs mount one chunk each. The host maps results to physical pages only
   through the manifest; citations and the viewer keep using the original.
3. Keep the current in-place path as the fallback (and for small PDFs). Refuse
   encrypted inputs. Record repairs or warnings in the manifest. Bound the
   chunk size. Add a size check for the shared-resource bloat.

Do not use pdfcpu (memory, startup, non-reproducible output). qpdf cannot run
on the pinned runtime. Native pdq (outcome B) measured best, but it would parse
untrusted evidence outside the WASI sandbox and outside Bacalhau's execution
record, the only non-Docker engines being WASM. It is acceptable only as an
optional accelerator with its own provenance record, like Docker. That is an
architecture decision, not a measurement.

**Not demonstrated / open:**

- Bounded memory for the preparation step itself. It needs ~2.3x the PDF in
  Bacalhau. **[I]** By that ratio a PDF above ~1.6 GiB would approach the
  4 GiB wasm32 linear-memory limit; this was not tested. The smallest
  experiment that removes the uncertainty: give the pdq WASI patch a
  `fd_pread`-backed source (`ReadAt` over the file instead of a `Vec`), or find
  why wazero doubles growth, and re-measure s10000/s30000 with
  `bacalhau_wasi.py`.
- pdq's failure to prune shared resources at scale (only shown on 12 pages):
  run `adversarial.py` on a large shared-resources fixture before adoption.
- Full 30,000-page OCR on chunks (deliberately not run; the 40-page decisive
  range plus k=1/2/4 concurrency were enough to compare). Interactive UI
  responsiveness. Cold-cache I/O, since every repeat ran on a warm OS cache.
- Long-term maintenance of the pdq patch: upstream pdq 0.5.1 is a young
  project, and the patch is 1 function.

## Defects found and fixed

Fixed in the experiment as received:

- `pdfcpu-go`: no `go.sum`, so the documented build and CI failed. pdfcpu
  creates a config directory under the default configuration, and helpers such
  as `PageCountFile` ignore the caller's config, so it panicked in WASI.
  `TrimFile` needs rename. Existence checks via `stat` corrupted `/out`.
- `prove-pdfcpu-wasi.ps1` (replaced by `bacalhau_wasi.py`): duplicated
  request, fixed ports, accepted any `ready`, judged the job state instead of
  the execution exit code, sampled only the node working set, never ran.
- `bench.py`: sampled root-process RSS only; the timeout left child processes
  alive; the qpdf derivative was non-reproducible. The CLI syntaxes were
  already correct: pdfcpu `trim --pages`, pdq `split IN --out RANGE PATH`,
  qpdf `--empty --pages IN A-B --`.
- CI only compiled. It now runs tests and the level-2 Wasmtime smoke test.

Found in my own additions during the work: an unclosed patch hunk, a single
huge read failing with ENOMEM under Wasmtime (now 64 KiB reads), the runner
memory guard that could never trip, and runner exceptions when a node died.
Each fix has a regression test.

## Incidents

Because of the guard defect above, two 30k WASI runs (`wasi-pdq-s30000-late40-r1`,
`wasi-pdfcpu-s30000-late40-r1`) were not aborted. They drove host available
RAM to ~6-7 MiB for ~4 s (pdq) and ~146 s (pdfcpu) on the shared workstation.
Both completed and the original was unchanged. After the fix, the guard aborted
a k=4 run at 516 MiB available as intended.

## Reproduction

See [README.md](README.md). The raw run directories (`.poc-runtime/pdf-chunker/`)
are not committed. `summarize.py` rebuilds `results/*.json` from them.
