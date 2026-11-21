# ADR-0003: Chunk large PDFs with flpdf in batch mode on Bacalhau + WASI

- **Status:** Accepted — implemented in PR [#17] (pending merge)
- **Date:** 2026-11-20
- **Accepted:** 2026-11-21 — project maintainer decision, on the experimental evidence.
- **Decision scope:** how VIII prepares very large PDFs (up to the validated 30,000 pages) before OCR
- **Project:** VIII — Digital Evidence Workbench
- **Depends on:** [ADR-0001](0001-adopt-go-wails-v3-bacalhau.md) (Bacalhau as sole orchestrator; WASI portable, Docker optional)
- **Evidence:** [PDF chunking overview](../poc/pdf/chunking.md) · PR [#13] (including merged PR [#15]) · issue [#14]

Implementation: issue [#16], PR [#17]
([product integration](../poc/pdf/chunking.md#product-integration)).
Until it is merged the MVP of PR [#12] keeps processing the original in place.

## Context

The MVP worker (Hayro + Tesseract, WASI) reads the whole PDF into linear
memory for every OCR job. On the 1.19 GiB, 30,000-page synthetic fixture one
40-page OCR job peaked at 2,890 MiB on the Bacalhau node, and four concurrent
jobs at 10,000 pages reached 4.1 GiB. Memory therefore grows with the document,
not with the work, and wasm32 linear memory is capped at 4 GiB.

Splitting the original into small page-range derivatives before OCR makes each
OCR job independent of the document size (270 MiB on a 40-page chunk, identical
OCR output). The question was which chunker can do this on the pinned runtime
without becoming the new memory bottleneck and without weakening provenance.

## Decision

Keep **Bacalhau + WASI** as VIII's initial and default path. Prepare large PDFs
with **flpdf 0.10.0** (Rust, MIT OR Apache-2.0, crate unmodified) as **one batch
WASI Preview 1 job per document** on Bacalhau 1.9.5. Keep **Docker** as an
optional accelerator, never a requirement.

The selected configuration:

- **Batch mode, `lean` extraction route.** The original is opened once and split
  into consecutive chunks using only flpdf's public API, with the inherited
  page-attribute push done once per batch instead of once per chunk. Chunks are
  byte-identical to `flpdf::extract_pages`.
- **On-demand reads** through `Read + Seek`; the original is never held in memory.
  Memory follows the document's page count, not its byte size.
- **Linear memory pre-sized to 128 MiB** for the validated scope of 30,000 pages
  (link argument `--initial-memory=134217728`). Pre-sizing avoids wazero's
  growth overhead.
- **Chunks of 40 pages initially**, as a parameter (`--chunk-size`).
- **Progressive delivery.** After each chunk is written and re-verified, the
  worker writes a sidecar (`chunk-AAAAAA-BBBBBB.json`: chunk sha256, exact
  local → physical page map). The host may copy, verify and use a chunk before
  the batch finishes. `manifest.json` (`viii.pdf-chunk-set.v1`) and `ready`
  come last; the worker waits for the host acknowledgement because Bacalhau
  deletes the result directory when the execution ends.
- **Chunks are operational derivatives, never evidence.** They are bound to the
  sha256 of the immutable original. OCR runs on chunks with the existing
  Hayro/Tesseract worker; every page result, search hit and citation is mapped
  back to the **physical page of the original**, and the viewer opens the original.
- **Manifest identity.** Today the manifest records tool and version, the declared
  original sha256, the physical range and per-chunk hash and map; the worker
  module's sha256 is recorded by the job runner. Integration must persist the
  module sha256, the Bacalhau job/execution IDs and the evidence identity with
  each chunk set.
- Encrypted input is **refused** by the worker until a policy exists.

## Rationale

1. **Predictable memory on large documents.** 30,000 pages / 1.19 GiB in batch:
   424 MiB node peak, against 2,737 MiB for pdq-WASI. A single 40-page range:
   161 MiB with a 96 MiB pre-sized module.
2. **Proven on the real executor.** Level 3 (pinned Bacalhau v1.9.5 on Windows,
   Docker disabled, network `none`, read-only input): 48/48 single-range jobs and
   every batch run completed and verified. A Wasmtime pass alone was shown to be
   misleading on this runtime.
3. **Portable without Docker.** One 5 MB WASI module; no native execution path
   outside Bacalhau (ADR-0001).
4. **Integrity and provenance.** Byte-reproducible chunks (native = Wasmtime =
   Bacalhau), qpdf-valid on 3,550 chunks, text and raster identical to the
   original's physical pages, original sha256 unchanged after every run, physical
   page 29,999 cited correctly after OCR of a chunk. Unreferenced resources are
   pruned and truncated-file repairs are recorded in the manifest.
5. **Lower maintenance than several chunkers.** One unmodified crate instead of a
   locally patched pdq plus a selection policy. Adaptive pdq/flpdf selection
   saved about 17 s per document in-sample, under 0.3% of the OCR time.
6. **Partial results before preparation ends.** First chunk usable on the host
   after 5.6 s at 30,000 pages; pdq can hand over chunks only after its whole
   batch (≥ 26 s).

The cost is preparation time: flpdf is 1.75–4.5× slower than pdq to prepare a
whole document (all 750 chunks verified at 101 s vs 58 s at 30,000 pages). OCR of
one 40-page chunk takes about two minutes, so preparation stays ahead of OCR and
the gap is off the critical path **[inference]**.

## Alternatives

| Option | Outcome | Status |
| --- | --- | --- |
| pdfcpu v0.16.4 (Go, WASI) | Runs on Bacalhau, but ~10 s and ~0.5 GiB startup per job, 6.3 GiB at 30,000 pages, non-reproducible output | Discarded |
| pdq 0.5.1 + local WASI patch | Fastest WASI batch (26 s ready), but holds the whole file: 2.7 GiB at 30,000 pages, ~1.6–1.8 GiB PDFs approach the 4 GiB limit; silently decrypts owner-password PDFs; loses repair warnings; copied every image into every chunk on one fixture | Discarded (was the first recommendation in PR [#13]) |
| pdq native | Best absolute numbers (6.3 s, 96 MiB private), but parses untrusted evidence outside the WASI sandbox and Bacalhau's record | Postponed: acceptable only as an optional accelerator with its own provenance, like Docker |
| qpdf 12.4.2 | Correct natively; cannot run on the pinned runtime (wazero v1.12.0 rejects Wasm exception handling) | Postponed until wazero supports Wasm EH |
| otf-pdf 0.2.0 | Runs on Bacalhau, but rescans the whole file on large classic xref tables and records false repair diagnostics | Postponed: needs an upstream fix |

## Consequences

- VIII gains a preparation stage, chunk storage (~0.95–1.0× the original on disk)
  and a chunk-set record per document. The original stays immutable and remains
  the only citation target.
- OCR job memory becomes independent of document size, which makes concurrent OCR
  jobs feasible on a workstation. Measured on the same chunks, 4 simultaneous jobs
  gave 3.4× the throughput of 1 with unchanged UI search latency, so the default is
  4 jobs on chunks (1 in place).
- flpdf is 0.x with essentially one author. Pin the version and checksum
  (`38cd9b8e…`) and use the adversarial suite plus the lean-route byte-identity
  check as the upgrade gate.
- The supported worker is `workers/pdf-chunker`, a copy of the measured
  experiment with product defaults; the experiment directory stays as the record.

## Open questions and limits

- Encrypted-PDF policy; silent repair of a lying xref is not detectable through
  flpdf diagnostics.
- Not measured: cold OS cache (flpdf requests 2.5–4× the file through the page
  cache), documents above 30,000 pages (~450 MiB linear memory at 100,000 pages
  is an extrapolation), the ~160 MiB node jump right after `ready` on large
  results, concurrency above 4 jobs, any other workstation.
- All fixtures are synthetic.

## Reconsider if

- Preparation becomes the end-to-end bottleneck (first lever: pruning and
  verification inside flpdf, not a second chunker).
- Accepted documents exceed the measured page count and the pre-sized memory.
- flpdf becomes unmaintained or an upgrade fails the gate.
- wazero gains Wasm exception handling (qpdf becomes possible) or Bacalhau's WASI
  runtime changes the measured memory behaviour.

[#11]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/11
[#14]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/14
[#16]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/16
[#12]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/pull/12
[#13]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/pull/13
[#15]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/pull/15
[#17]: https://github.com/vmenezesdev/viii-digital-evidence-workbench/pull/17
