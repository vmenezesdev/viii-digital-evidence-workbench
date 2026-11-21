# Chunked PDF processing: raw results

Absolute workspace paths are redacted to `<workspace>`.

Produced by the scripts in `scripts/` on the Windows PoC workstation
(Windows 11 10.0.26300, 12 logical / 8 physical cores, 15.7 GiB RAM shared
with desktop applications), pinned Bacalhau v1.9.5, unchanged OCR worker
`ed7b4f04…`. Summarised in [the chunking overview](../../chunking.md).

| Directory | What | Status |
| --- | --- | --- |
| `concurrency-02/` | 1/2/4 simultaneous WASI OCR jobs, 8 verified chunks of the 30k fixture (pages 29,681–30,000, 33 scanned), `scripts/benchmark-pdf-chunk-concurrency.ps1`. No other workload ran. | **Decisive** for the default of 4 jobs |
| `wasi-30000/` | Full product run, 30,000-page fixture, WASI, 4 page jobs, package `bin/pdf-chunked` (VIII `cb41823c…`, built from `5f70ad5`). `summary.json`, compressed `events.jsonl.gz` (5 s samples; the collector's `state.json`, which repeats the fixture manifest, is omitted), chunk-set record, late chunk sidecar, page 29,999 record. | Passed |
| `docker-30000-failed-01/` | First Docker run, package `bin/pdf-chunked-02`. Extraction completed (750 jobs); at the start of OCR Bacalhau rejected a page job (`not enough nodes to run job`: 2 GB reserved per job, 546 MB left on the Docker node) and the product paused. `rejected-attempt.json` holds the scheduler message. | **Failed**, kept as evidence; fixed in `8be1d82` |
| `docker-30000/` | Fresh Docker run, package `bin/pdf-chunked-03` (VIII `ae05713c…`, built from `8be1d82`: 1 GB per chunk job, capacity rejections absorbed). 2 h 30 min, 0 failed pages. Its 2.66 GB Docker-volume staging led to `54fc169`, validated by `gates/chunked-product-docker-04` (empty `control/` and `out/` after the gate). | Passed |
| `docker-30000-rerun/` | Docker run with package `bin/pdf-chunked-04` (built from `54fc169`, per-pass staging cleanup) to validate the cleanup at scale: passed, 0 failed pages, Docker volume ≤ 61 MiB (was 2.5 GiB), but 175.6 min instead of 149.9 (extraction 64.0 vs 47.2 min, OCR 1,612 vs 1,750 pages/h) because each pass started a helper container. Led to batched cleanup in `76213db`, validated by the 12-page gate `gates/chunked-product-docker-05`. | Passed; cost recorded |
| `ui-29999/` | Product UI in Chrome on the completed WASI profile: search, real local-model answer, citation of physical page 29,999 and viewer screenshots at 30 s and 90 s. | Passed |
| `gates/` | 12-page product gates: WASI, Docker, live restart + switch (`bin/pdf-chunked`), and restricted-network first use without Docker or toolchains on PATH (`bin/pdf-chunked-02`); Docker and switch repeated with `bin/pdf-chunked-03`. | Passed |
| `package-identity.json` | Audit of `bin/pdf-chunked`. | Passed |
| `concurrency-01/` | Same method, 4 chunks (29,841–30,000, 17 scanned). | **Contaminated**: Rust builds ran during k=2, and with 4 chunks k=4 measures one wave (slowest job), not steady state. Kept as a record, not used for the decision. |

Each `jobs-N/` holds `summary.json`, `events.jsonl.gz` (≈1 s samples: node
working set/private bytes, host CPU and free memory, active job count) and
`ui.jsonl` (search latency on a completed local index in headless Chrome,
about once per second). Peaks are sampled lower bounds; all runs used a warm
OS cache. `identity.json` records chunk, worker, model and Bacalhau hashes.
