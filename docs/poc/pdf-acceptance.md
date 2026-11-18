# PDF MVP acceptance evidence

This is an in-progress audit against `MVP-PDF.md`, not a completion declaration.
Only synthetic PDFs were used. The source diff remains on the current branch.
An [intermediate snapshot](results/pdf-mvp/intermediate-20261118.json) at
2026-11-18 14:09:52 UTC records 27,198 searchable pages, including 199 OCR pages,
2,802 pending and zero failures. Full acceptance remains incomplete.
The latest [complete source test run](results/pdf-mvp/product-all-tests-12.log)
includes persisted-submission integrity and saved-node identity regressions.

| Requirement | Current evidence | Assessment |
| --- | --- | --- |
| Real Portuguese OCR on pinned Bacalhau Windows WASI | [Technical journal](pdf-mvp.md), real worker artifacts and pinned revision | Passed |
| Existing PDF text and selected physical-page rendering in WASI | `pages-render-proof`, `pages-selected-proof` under [results](results/pdf-mvp) | Small PDF passed; full large memory/throughput result pending |
| SHA identity, local read-only mount after authorization | Import/placement/integrity regressions in `pdf_document_test.go`; runtime input specs; actual original SHA checks in small product drivers | Passed small product gate |
| Incremental digital extraction, OCR only for insufficient text, one raster at a time | Actual mixed outputs, early search events, worker source; digital observations separate from final OCR records | Passed small product gate; large run active |
| Unified physical-page status, source, tool/version, engine and execution provenance | Archived `page-000008.json` / `page-000011.json` from WASI, Docker and switch product runs | Passed |
| Confirmed pages searchable during processing, unreadable pages visible | Native partial-search event/screenshot; final native UI shows 11 searchable and 1 unreadable; UI also exposes failure-page links | Passed; latest recovery changes need final packaged smoke test |
| Reconcile live jobs on reopen; cancel and resolve WASI before Docker reassignment | Switch driver events and immutable-page hashes; [actual terminal ordering](results/pdf-mvp/product-switch-01/lifecycle-order.json) with archived attempt records; regression for a still-running execution | Passed small live gate |
| Idempotent pages and only missing work after a crash | Canonical page store + range skip tests; unsubmitted-intent, metadata-before-ACK, held-Windows-reader and durable-page-before-index regressions; large run reopened at page 1154 with 2191 artifact hashes unchanged | Source/Rust tests and rebuilt small package passed; large recovery observed; final audit pending |
| Docker unavailable keeps WASI enabled | PATH-isolated regression verifies explicit guidance and unchanged WASI engine state | Passed |
| Local search, one real inference backend, retrieved citations and physical original page | Qwen/llama.cpp real answer; native viewer screenshot and citation `#page=11`; original response SHA check | Passed |
| Partial or missing evidence made explicit | Missing-query/local-backend tests, native answer partial-index flag | Passed |
| Bundled WASI worker and Portuguese model, no-Docker offline first use | Restricted-network CLI/HTTP full workflow; separate fresh native profile with dead external proxy, failed external network control, minimal Windows PATH, local search/OCR/real inference | Passed with test-method limits described in [product report](pdf-product.md) |
| Both engines on representative 30,000-page mixed PDF | Fixed 1.25 GB fixture, 3,001 scans; scalar partial run preserved, fresh full SIMD run active after paired probes and small gate; Docker queued | Incomplete |
| First useful search, OCR throughput, total time, RAM and temporary disk | Resumable event samples; machine/package identity; real page-1 hit at 55.168 s from import; sampled Windows peaks and separate Docker metrics supported by runner | Initial scalar timestamp invalidated. SIMD run includes a documented I/O failure, configuration change and recovery; final summaries pending |
| Compare 1/2/4 WASI jobs and machine responsiveness | Prepared 40-page/5-scan equal-workload runner, actual overlapping execution counts, repeated product UI searches and scheduler-delay probe | Not run yet; queued after large runs |
| Keep default concurrency 1 unless measurements justify change | Production scheduler submits one current range per document | Default remains 1 |
| Regression coverage and repeat commands | [Product report](pdf-product.md), Go test logs, functional/large/concurrency scripts | Commands available; final all-source validation and package audit pending |

The current Windows benchmark [package hashes](results/pdf-mvp/package-identity-continue-20261118.json)
identify the rebuilt desktop and unchanged bounded-I/O SIMD worker, OCR model
and pinned Bacalhau runtime. The earlier machine record remains historical.
The worker's full small product gate passed.
Final delivery still requires the large-file/native-UI and packaging audit.
The large runs and concurrency comparison must finish before marking the goal
complete. No reduction of the 30,000-page workload is used to satisfy that gate.
