# In-place PDF MVP: raw results

Evidence for the first, in-place version of the PDF workflow (PR #12, before chunking).
Read [the product report](../../pdf-product.md) and [the technical journal](../../pdf-mvp.md)
first; the current acceptance is in [pdf-acceptance.md](../../pdf-acceptance.md) and
[`../pdf-chunked`](../pdf-chunked/README.md).

| Files | What they show |
| --- | --- |
| `ocr-proof/`, `ocr-attempt-1..8.log` | Portuguese OCR in WASI on pinned Bacalhau. Attempts 1–6 are the failures on the way (missing parameters, `/out` not absolute, publisher required, OCR job failures); 7 is the passing proof, 8 the artifact test. |
| `pages-*-proof/`, `pages-*attempt*.log`, `stream-*.log`, `pages-docker-*` | Text extraction and selected physical-page rendering, WASI and Docker, including the streamed variants. |
| `build-attempt-1..6.log`, `pdf-build-1.log`, `pdf-build-4.log`, `*-build.log` that remain | Worker builds that failed or needed changes (dependency conflicts, toolchain issues), kept as failure records; the three `simd-*-build.log` are cited by `simd-experiment-annotation.json`. |
| `product-*-01/`, `product-*.log`, `answer-proof/`, `answer-attempt-1.log` | Small product gates: WASI, Docker, live switch, restricted-network first use, native WebView2 UI (including its failed connection attempts and one failed assertion). |
| `product-all-tests-12.log` | Last full Go regression run of this stage. |
| `benchmark-*`, `intermediate-*`, `scalar-*`, `simd-*`, `user-*`, `resume-bounded-io.log`, `supersede-scalar.log`, `hidden-filehash-*.json`, `collector-array-regression.log`, `cli-*` | History of the in-place 30,000-page run: invalid first timestamp, I/O failure and diagnosis, SIMD adoption, scalar run superseded, user pauses, relocation, CLI lifecycle checks. Never completed; superseded by the chunked runs. |
| `fixture-*` | Synthetic fixture generation. |

On 2026-11-21, 64 logs without evidential content were removed from this directory:
30 empty logs, passing Go test reruns superseded by `product-all-tests-12.log`, and
passing build, download and dependency logs. Every log with a failure and every file cited by the
documentation or by another evidence record was kept. The removed files remain in the
history at commit
`bd33e5d` (`git show bd33e5d:docs/poc/results/pdf-mvp/<file>`).
