# Large-PDF chunking: overview and current state

**Status (2026-11-20):** chunker selected by experiment ([ADR-0003](../adr/0003-chunk-large-pdfs-with-flpdf-wasi.md),
Proposed). **Not integrated into VIII.** Integration: issue [#16]. Experiment: PR [#13]
(includes merged PR [#15]), issue [#14]. MVP: PR [#12], issue [#11].

This page is the entry point. Details and every number's source are in the linked
reports; nothing here needs the historical discussion to be read first.

## Problem

VIII must let a lawyer import a PDF of up to 30,000 pages, search it while it is
processed and get answers that cite the original's physical pages, running on
Bacalhau + WASI without Docker ([MVP-PDF.md](../../MVP-PDF.md)). The MVP's OCR worker
(Hayro + Tesseract, WASI) loads the **whole PDF** into linear memory on every job:
2,890 MiB node peak for one 40-page job on the 1.19 GiB fixture, and wasm32 memory
stops at 4 GiB. Chunking splits the original into small page-range derivatives so
that OCR memory follows the chunk, not the document.

## What VIII can do today (PR [#12], not merged)

Demonstrated on the Windows workstation with Bacalhau 1.9.5 ([product report](pdf-product.md),
[acceptance matrix](pdf-acceptance.md)):

- Real Portuguese OCR in WASI on the pinned Bacalhau executor.
- Import with SHA-256 identity, authorized read-only placement, incremental text
  extraction and FTS5 search while OCR continues.
- Recovery after interruption and reopen with accepted page hashes unchanged; switch
  from WASI to Docker without duplicate or overwritten pages.
- Local inference (llama.cpp) whose citations open the original's physical page.

**Not demonstrated:** the full 30,000-page run on either engine, the 1/2/4-job
comparison and the final package audit. The last committed snapshot of the 30k WASI
run has 27,198 pages searchable and 2,802 pending ([snapshot](results/pdf-mvp/intermediate-20261118.json)).
The MVP does **not** use chunks.

## What the experiments demonstrated

Same workstation for all runs: Windows 11 10.0.26300, 12 logical / 8 physical cores,
15.7 GiB RAM shared with desktop applications (0.6–5.6 GiB available during runs),
warm OS cache. Pinned Bacalhau v1.9.5 (`3a6a73aa…`, wazero v1.12.0), Docker disabled,
network `none`, read-only input, one fresh node per run. Memory on Bacalhau = node
lifetime peak minus idle baseline (includes ~89 MiB of wazero compiling the flpdf
module). Fixtures are synthetic; the decisive one is `mixed-30000.pdf` (30,000 pages,
1,193 MiB, 10% scanned, `JABUTICABA` on scanned physical page 29,999, sha256 `eb3ca405…`).

The three measurements below are **different operations**; do not compare their
numbers across tables.

**1. Single range, late 40 pages of the 30k fixture** ([ALTERNATIVES.md](../../experiments/pdf-chunker/ALTERNATIVES.md))

| Chunker | Node peak | Until `ready` |
| --- | ---: | ---: |
| pdq 0.5.1 + WASI patch | 2,719 MiB | 4.6–7.3 s |
| flpdf 0.10.0, default module | 390 MiB | 6.3 s |
| **flpdf 0.10.0, `--initial-memory` 96 MiB** | **161 MiB** | 5.3 s |

48/48 flpdf Bacalhau jobs (12 → 30,000 pages; ranges p1, p32, late40 and the text-only
30k file; two modules; two repeats)
completed and verified; chunks have the same sha256 natively, on Wasmtime and on Bacalhau.

**2. Whole-document batch, 750 chunks of 40 pages** ([BATCH-RESULTS.md](../../experiments/pdf-chunker/BATCH-RESULTS.md))

| 30k fixture | pdq WASI (historical, PR [#13]) | **flpdf WASI, lean, 128 MiB** |
| --- | ---: | ---: |
| Node peak | 2,737 MiB | **424 MiB** |
| First chunk verified and usable on host | ≥ 26 s (only after `ready`) | **5.6 s** |
| Job until `ready` | 26.2 s | 78.2 s |
| All chunks verified, then ack | 57.6 s | 100.8 s |

pdq is 1.75–4.5× faster to prepare a whole document in every measured scenario and uses
less memory only on many light pages (19 MiB text-only 30k file: 195 vs 248 MiB).

**3. OCR on a chunk** (unchanged MVP worker `ed7b4f04…`)

| Input | OCR job peak | OCR wall | Source |
| --- | ---: | ---: | --- |
| original, pages 29,961–30,000 | 2,890 MiB | 142.0 s | [RESULTS.md](../../experiments/pdf-chunker/RESULTS.md) |
| pdq chunk of the same pages | 270 MiB | 129.5 s | RESULTS.md |
| flpdf batch chunk of the same pages | 276 MiB | 124 s | BATCH-RESULTS.md |

On both chunks `JABUTICABA` was found on chunk-local page 39 and cited as **physical
page 29,999 of the original**; 40/40 pages completed.

**Integrity** ([BATCH-RESULTS.md, Forensic integrity](../../experiments/pdf-chunker/BATCH-RESULTS.md#forensic-integrity-f)):
original sha256 unchanged after all runs; one set digest per scenario across 154 batch
runs; `qpdf --check` passed on 3,550 chunks; Poppler text and 30-dpi raster identical to
the original's physical pages (all 1,000 pages of s1000, sampled boundaries at 10k/30k);
no fixture text in job logs.

## Why flpdf

Reads the original on demand (`Read + Seek`), so memory follows page count rather than
bytes; runs unmodified on the real executor; one portable module; byte-reproducible,
pruned chunks with repair diagnostics; and it hands over chunks progressively, so OCR
can start seconds after import. Full rationale: [ADR-0003](../adr/0003-chunk-large-pdfs-with-flpdf-wasi.md#rationale).

## Why not pdq

pdq WASI holds the whole file: 2.7 GiB at 30,000 pages, and PDFs of ~1.6–1.8 GiB would
approach the 4 GiB limit. It could not even be re-run on this workstation (admission
refused it with 0.6–3.1 GiB available). It also silently decrypts owner-password PDFs,
loses repair warnings in Bacalhau and copied every image into every chunk on one fixture.
Its speed advantage (≤ 55 s per 30k document) is under 0.3% of that document's OCR time.
Keeping it as a second, adaptively selected strategy was measured and rejected.
pdfcpu, qpdf, otf-pdf and native pdq: see the [alternatives table](../adr/0003-chunk-large-pdfs-with-flpdf-wasi.md#alternatives).

## Intended architecture

```
import original (sha256, immutable)
  └─ Bacalhau WASI job: flpdf batch, 40-page chunks ──► chunk-*.pdf + chunk-*.json sidecars … manifest.json, ready
        host: copy + verify each chunk (hash, count, local→physical map) while the job waits; ack at the end
  └─ chunk store: operational derivatives bound to the original's sha256 (never evidence)
  └─ Bacalhau WASI OCR jobs (Hayro/Tesseract) on chunks already verified, while preparation continues
  └─ page records, FTS5 index, citations and viewer: physical pages of the ORIGINAL only
Docker: optional acceleration of the same jobs; never required.
```

## Limitations

- Host integration does not exist (issue [#16]).
- Encrypted PDFs are refused (no policy yet); a lying xref is repaired silently.
- Not measured: cold cache, > 30,000 pages, other workstations, UI responsiveness during
  preparation, concurrent OCR on chunks, the ~160 MiB node jump after `ready` (cause not traced).
- Synthetic fixtures only. flpdf is 0.x with one main author.

## What is left for the product

One issue, [#16]: supported worker, progressive sidecar consumption, copy-and-verify before
deletion, chunk store bound to the original, OCR on available chunks, physical-page mapping,
idempotence/resume/cancellation/history and regression of citations, index and integrity
(P0); encryption policy, repair reporting, responsiveness/concurrency/cold-cache, another
workstation and > 30k pages (P1). The MVP's own gates remain in [#11].

## Reproduce the decisive results

Tools, fixtures and full commands: [experiment README](../../experiments/pdf-chunker/README.md#reproduction-windows-poc-machine),
[ALTERNATIVES.md, Reproduction](../../experiments/pdf-chunker/ALTERNATIVES.md#reproduction) and
[BATCH-RESULTS.md, Reproduction](../../experiments/pdf-chunker/BATCH-RESULTS.md#reproduction). The decisive ones:

1. Build `flpdf-wasi` with `--initial-memory=134217728`; the module sha256 must be
   `3cf7c9ff…` (rebuilt and matched on 2026-11-20).
2. `batch_bench.py wasi … --fixture s30000=…` for the 750-chunk batch (node peak, first
   chunk, `ready`, all verified).
3. `e2e_ocr.py --first 29961 --last 30000 --expect-page 29999` for the chunk → OCR →
   physical citation gate.

Level 3 (real Bacalhau) is a local gate; CI covers compilation, tests and Wasmtime
smoke runs of the single-range and batch modes.

## Evidence map

| Document | Responsibility |
| --- | --- |
| [ADR-0003](../adr/0003-chunk-large-pdfs-with-flpdf-wasi.md) | The decision, rationale and alternatives |
| This page | Current state and entry point |
| [RESULTS.md](../../experiments/pdf-chunker/RESULTS.md) | Round 1: pdfcpu, pdq, qpdf; Bacalhau WASI quirks; OCR-on-chunk and concurrency (its pdq recommendation is superseded) |
| [ALTERNATIVES.md](../../experiments/pdf-chunker/ALTERNATIVES.md) | Round 2: on-demand I/O; flpdf, otf-pdf; wazero growth explained (its "batch not implemented" point is resolved) |
| [BATCH-RESULTS.md](../../experiments/pdf-chunker/BATCH-RESULTS.md) | Round 3: batch pdq vs flpdf, lean route, adaptive selection, integrity |
| [`results/*.json`](../../experiments/pdf-chunker/results/) | Structured results each report is built from (`summarize.py`, `alt_summarize.py`, `batch_summarize.py`) |
| [pdf-product.md](pdf-product.md), [pdf-acceptance.md](pdf-acceptance.md) | MVP product results and acceptance (PR [#12]) |

Raw run directories (`.poc-runtime/`) are not committed and exist only on the PoC
workstation; the committed JSON files are the preserved record.

**Sequence of findings.** (1) pdq-WASI works on Bacalhau and makes OCR memory
independent of the document, but its preparation job needs ~2.3× the file. (2) flpdf
reads on demand and pre-sizing linear memory removes wazero's growth overhead: 161 MiB
for one range. (3) In batch, flpdf stays at 424 MiB for the whole 30k document and
delivers the first chunk in 5.6 s; pdq is faster overall but memory-bound; one strategy
is enough.

**Incidents kept as evidence.** A runner memory-guard defect let two 30k WASI runs drive
host available RAM to ~6–7 MiB (fixed, regression-tested; [RESULTS.md, Incidents](../../experiments/pdf-chunker/RESULTS.md#incidents)).
In the batch round the guard killed three native 30k runs, admission refused every pdq
WASI 30k run, and the first OCR-on-chunk attempt was killed when other applications left
1.0 GiB available; none was retried with a weaker guard ([BATCH-RESULTS.md, Method](../../experiments/pdf-chunker/BATCH-RESULTS.md#method)).
The MVP's own run history (I/O error, invalid first-search timestamp, pauses, relocation)
is in [pdf-product.md](pdf-product.md).

[#11]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/11
[#14]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/14
[#16]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/16
[#12]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/pull/12
[#13]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/pull/13
[#15]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/pull/15
