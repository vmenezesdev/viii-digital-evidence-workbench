# In-place PDF MVP: raw results

Evidence for the first, in-place version of the PDF workflow (before chunking). Read
[the product report](../../product.md) and [the technical journal](../../journal.md)
first; the current acceptance is in [acceptance.md](../../acceptance.md) and
[`../chunked`](../chunked/README.md).

| Folder | What it shows |
| --- | --- |
| [`ocr/`](ocr) | Portuguese OCR in WASI on pinned Bacalhau. `ocr-attempt-1..6.log` are the failures on the way (missing parameters, `/out` not absolute, publisher required, OCR job failures); 7 is the passing proof, 8 the artifact test; `ocr-proof/` holds the job record. |
| [`pages/`](pages) | Text extraction and selected physical-page rendering, WASI and Docker, including the streamed variants. |
| [`builds/`](builds) | Worker builds that failed or needed changes (dependency conflicts, toolchain issues), kept as failure records; the journal's "Failed attempts" section explains each. |
| [`product-gates/`](product-gates) | Small product gates: WASI, Docker, live switch, restricted-network first use, native WebView2 UI (with its failed connection attempts and one failed assertion), the real-model answer proof, and `product-all-tests-12.log`, the last full Go regression run of this stage. |
| [`cli/`](cli) | Detached experiment CLI lifecycle checks ([experiment-cli.md](../../experiment-cli.md)). |
| [`large-run/`](large-run) | History of the in-place 30,000-page run: invalid first timestamp, I/O failure and diagnosis, SIMD adoption and its builds, scalar run superseded, user pauses, relocation, intermediate snapshot. Never completed; superseded by the chunked runs. |
| [`fixtures/`](fixtures) | Synthetic fixture generation. |

On 2026-11-21, 64 logs without evidential content were removed: 30 empty logs,
passing Go test reruns superseded by `product-all-tests-12.log`, and passing build,
download and dependency logs. Every log with a failure and every file cited by the
documentation or by another evidence record was kept. The removed files remain in the
history at commit `bd33e5d`, under their former path
(`git show bd33e5d:docs/poc/results/pdf-mvp/<file>`).
