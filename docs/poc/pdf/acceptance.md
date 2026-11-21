# PDF MVP acceptance evidence

Audit against [`MVP-PDF.md`](MVP-PDF.md) on 2026-11-21, after large PDFs moved to
verified chunks ([ADR-0003](../../adr/0003-chunk-large-pdfs-with-flpdf-wasi.md), PR #17).
Only synthetic PDFs were used, on one shared Windows workstation (12 logical / 8 physical
cores, 15.7 GiB RAM). Details and every number's source:
[chunking overview](chunking.md#product-integration) and
[`results/chunked`](results/chunked/README.md). The earlier in-place evidence
(PR #12) is kept in [the product report](product.md) and
[`results/mvp`](results/mvp/README.md).

| Requirement | Evidence | Assessment |
| --- | --- | --- |
| Real Portuguese OCR on pinned Bacalhau Windows WASI | [Technical journal](journal.md); 3,001 OCR pages in the 30k WASI run with the unchanged worker `ed7b4f04…` | Passed |
| Existing PDF text and selected physical-page rendering in WASI, memory suited to the document | 30k WASI run: 26,999 digital + 3,001 OCR pages through 40-page chunks; sampled app + node peak 3.0 GiB (in place: 5.5 GiB working set, 6.8 GiB private) | Passed |
| SHA identity, local read-only mount after authorization | Import/placement/integrity regressions; chunks bound to the original's sha256 and rejected otherwise (`pdf_chunks_test.go`); original hash unchanged after both 30k runs | Passed |
| Incremental digital extraction, OCR only for insufficient text, one raster at a time | Extraction phase then OCR of the 3,001 scanned pages only, both engines | Passed |
| Unified physical-page status, source, tool/version, engine and execution provenance | `page-029999.json` from the WASI and Docker runs; chunk records with job, execution and chunker identities | Passed |
| Confirmed pages searchable during processing, unreadable pages visible | First useful search with ~29,900 pages pending: 107.6 s (WASI, cold upload) and 27.4 s (Docker) from import start; 12-page gates show 1 unreadable page | Passed |
| Reconcile live jobs on reopen; cancel and resolve WASI before Docker reassignment | Live restart + WASI→Docker switch gate on chunks (`gates/chunked-product-switch-03`); pause/cancel/resume regressions for preparation and page jobs | Passed |
| Idempotent pages and only missing work after a crash | Durable page/chunk records, resume of preparation after the last verified chunk, legacy-state migration, restart gate | Passed |
| Docker unavailable keeps WASI enabled | PATH-isolated regression | Passed |
| Local search, one real inference backend, retrieved citations and physical original page | Product UI on the completed 30k profile: real Qwen answer citing physical page 29,999; viewer at 29999 / 30000 (`ui-29999`) | Passed (browser; native WebView2 checked earlier on the 12-page PDF) |
| Partial or missing evidence made explicit | Missing-query/local-backend tests, partial-index flag | Passed |
| Bundled WASI worker and Portuguese model, no-Docker offline first use | Package audit (`package-identity.json`); restricted-network first use with the chunked package (`gates/chunked-offline-01`) | Passed with the test-method limits in [the product report](product.md) |
| Both engines on a representative 30,000-page mixed PDF | 1.25 GB fixture, 3,001 scans: WASI 6 h 04 min, Docker 2 h 30 min, 0 failed pages, term on physical page 29,999 found | Passed (Docker on the second attempt; the first paused on a scheduler rejection, kept and fixed) |
| First useful search, OCR throughput, total time, RAM and temporary disk | [Full-run table](chunking.md#product-integration); collector samples RAM every 5 s, disk every 30 s (lower bounds; Docker VM memory separate) | Passed |
| Compare 1/2/4 WASI jobs and machine responsiveness | Same 8 chunks: 666 / 347 / 196 s (3.40× at 4), UI search p95 62–64 ms, node ≤ 1,008 MiB (`concurrency-02`) | Passed |
| Change default concurrency only if justified | Default 4 page jobs on chunks (1 in place), from the measurement above | Changed, justified |
| Regression coverage and repeat commands | `go test ./...`, quality and chunker CI, `validate-pdf-product.ps1`, `pdf-experiment.ps1 -Jobs`, `benchmark-pdf-chunk-concurrency.ps1` | Passed |

Not covered: real (non-synthetic) evidence, another workstation, documents above 30,000
pages, cold-cache measurements beyond the one cold upload, and encrypted PDFs (refused by
the chunker; in-place fallback only after two failed preparations).
