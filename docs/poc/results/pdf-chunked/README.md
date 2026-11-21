# Chunked PDF processing: raw results

Absolute workspace paths are redacted to `<workspace>`.

Produced by the scripts in `scripts/` on the Windows PoC workstation
(Windows 11 10.0.26300, 12 logical / 8 physical cores, 15.7 GiB RAM shared
with desktop applications), pinned Bacalhau v1.9.5, unchanged OCR worker
`ed7b4f04…`. Summarised in [the chunking overview](../../pdf-chunking.md).

| Directory | What | Status |
| --- | --- | --- |
| `concurrency-02/` | 1/2/4 simultaneous WASI OCR jobs, 8 verified chunks of the 30k fixture (pages 29,681–30,000, 33 scanned), `scripts/benchmark-pdf-chunk-concurrency.ps1`. No other workload ran. | **Decisive** for the default of 4 jobs |
| `wasi-30000/` | Full product run, 30,000-page fixture, WASI, 4 page jobs, package `bin/pdf-chunked` (VIII `cb41823c…`, built from `5f70ad5`). `summary.json`, `state.json`, compressed `events.jsonl.gz` (5 s samples), chunk-set record, late chunk sidecar, page 29,999 record. | Passed |
| `docker-30000/` | Same workload with the Docker engine, package `bin/pdf-chunked-02` (VIII `e2ee4ad0…`, built from `4c10a35`, viewer fix only). | See its summary |
| `ui-29999/` | Product UI in Chrome on the completed WASI profile: search, real local-model answer, citation of physical page 29,999 and viewer screenshots at 30 s and 90 s. | Passed |
| `gates/` | 12-page product gates: WASI, Docker, live restart + switch (`bin/pdf-chunked`), and restricted-network first use without Docker or toolchains on PATH (`bin/pdf-chunked-02`). | Passed |
| `package-identity.json` | Audit of `bin/pdf-chunked`. | Passed |
| `concurrency-01/` | Same method, 4 chunks (29,841–30,000, 17 scanned). | **Contaminated**: Rust builds ran during k=2, and with 4 chunks k=4 measures one wave (slowest job), not steady state. Kept as a record, not used for the decision. |

Each `jobs-N/` holds `summary.json`, `events.jsonl` (≈1 s samples: node
working set/private bytes, host CPU and free memory, active job count) and
`ui.jsonl` (search latency on a completed local index in headless Chrome,
about once per second). Peaks are sampled lower bounds; all runs used a warm
OS cache. `identity.json` records chunk, worker, model and Bacalhau hashes.
