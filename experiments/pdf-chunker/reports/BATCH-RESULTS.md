# Batch PDF chunking: pdq vs flpdf, and adaptive selection

> **Round 3 of 3.** Decision recorded in [ADR-0003](../../../docs/adr/0003-chunk-large-pdfs-with-flpdf-wasi.md) (Accepted); VIII host integration
> is [issue #16](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/16). Current state: [large-PDF chunking overview](../../../docs/poc/pdf/chunking.md).

Branch `experiment/pdf-chunker-batch-selection` (from `experiment/pdf-chunker-wasi`
@ `175e428`, PR #13, issue #14). Measured on 2026-11-20 on the same Windows
workstation, fixtures and pinned Bacalhau v1.9.5 / wazero v1.12.0 as
[RESULTS.md](RESULTS.md) and [ALTERNATIVES.md](ALTERNATIVES.md). Every number
comes from [`results/batch.json`](../results/batch.json) unless it is marked as
historical. Units: MiB/GiB, seconds. Labels: **[F]** measured fact, **[I]**
inference from measurements, **[H]** hypothesis not demonstrated.

## Answer

**Recommendation: A, flpdf as the single WASI chunker** (batch mode, `lean`
extraction route, module pre-sized to 128 MiB), with the conditions in
[Decision](#decision). The decision is an inference. The facts behind it:

| 30,000 pages / 750 chunks, Bacalhau | pdq WASI | flpdf WASI (lean) |
|---|---:|---:|
| mixed 1.19 GiB (s30000): node peak | 2,737 MiB (historical) | **424 MiB** |
| s30000: job until `ready` / all chunks verified on host | 26.2 / 57.6 s (historical) | 78.2 / 100.8 s |
| s30000: first chunk usable on host | after `ready`: >= 26 s | **5.6 s** |
| text-only 19 MiB (t30000): node peak | **195 MiB** | 248 MiB |
| t30000: `ready` / all verified | **15.3 / 26.3 s** | 68.7 / 80.9 s |

- pdq is 1.7-4.5x faster in every measured scenario **[F]**. Its memory grows
  with the file size (whole file in linear memory). The 30k mixed document
  could not be repeated here: the admission check refused it every time
  (needs ~2.7 GiB + margin; 0.6-3.1 GiB was available) **[F]**.
- flpdf uses 5-6.5x less memory on heavy documents (s10000, s30000) and
  1.3x more on many light pages (t30000) **[F]**. It is the only one that
  can hand chunks to the host before the batch ends (first chunk usable in
  2.5-6 s).
- pdq's advantage is at most 55 s of preparation per 30,000-page document
  **[F]**. One OCR job on 40 of those pages takes 124 s **[F]**, so the
  full OCR of such a document takes hours. Since flpdf's first chunk
  arrives first and it produces chunks faster than OCR consumes them, the
  gap does not reach the end-to-end time **[I]**. Keeping pdq would buy
  that minute at the cost of a second code path that silently decrypts
  owner-password PDFs, loses repair warnings in Bacalhau and, on some files,
  copies every image into every chunk **[F]**.

## What was built

| Path | What |
|---|---|
| `flpdf-wasi/` | Batch mode added to the existing worker: opens the original once, extracts, prunes, writes and verifies each chunk in turn (one extracted document alive at a time), writes a sidecar `chunk-A-B.json` after each verified chunk, then `manifest.json` (`viii.pdf-chunk-set.v1`, pdq's schema) and `ready`; waits for the ack. Contracts: pdq's positional `orig first last out ack sha size` and `orig --chunk-size N --out-dir D [--range A-B] [--ack F] [--source-sha256 S]`. Per-chunk and per-phase timings, guest I/O counters and linear memory in the manifest. Opt-in `FLPDF_EXTRACT=lean` route (below). Encrypted input still refused. flpdf 0.10.0 unmodified. |
| `batch_bench.py` | Guarded comparison of the three alternatives: native (exact OS peaks from the process handle) and Bacalhau (existing runner). Admission check before each run (estimate + 1 GiB margin vs available RAM), host guard at 1 GiB, job memory limit, timeout, original sha256 before/after. |
| `bacalhau_wasi.py` | `progressive`: copies and verifies the first chunk as soon as its sidecar exists; records `copy_verify_s` (ready -> ack); `Task.Env` for job variables; fix for a race when the memory guard kills the node. |
| `batch_validate.py` | qpdf `--check`/`--show-npages` on every chunk; Poppler text + 30-dpi raster vs the original's physical pages for all or sampled chunks (both sides of a boundary); cross-implementation page equality; size outliers. |
| `batch_adversarial.py` | Adversarial cases at 1,000 pages in batch mode, both tools. |
| `selection_model.py`, `batch_summarize.py` | Cost model, selection policy under test, fit and replay; builds `results/batch.json`. |
| `test_batch.py` | Runner/harness/policy tests, plus worker tests on a native build (tiling, sidecars, physical map, lean byte-identity, pdq/flpdf boundary parity, chunk-boundary text). |

## Method

- Fixtures (sha256 checked before and after every run, never modified):
  s1000 (1,000 p, 40 MiB), s10000 (10,000 p, 398 MiB), t30000 (30,000 text
  pages, 19.4 MiB, one scanned page), s30000 (MVP `mixed-30000.pdf`,
  30,000 p, 1,193 MiB, 10% scanned). Chunk size 40. Intermediate points:
  10 and 50 chunks (pages 1-400, 1-2,000).
- **A. pdq native batch**: `pdq-wasi` built for the host. Its WASI patch is
  `cfg(wasi)` only, so this is pdq 0.5.1 with its own mmap loader, called
  through `pdq::split` with all outputs in one call (the CLI's batch path).
  Not a production candidate.
- **B. pdq WASI batch**: the existing module, rebuilt byte-identically
  (`1799189b`). The 30k historical baseline used the previous build
  (`9316f81b`, before the 64 KiB read fix).
- **C. flpdf WASI batch**: `library` route (`flpdf::extract_pages`) and
  `lean` route (+ 2 KiB read buffer), same module. s10000 used the default
  module; s1000, t30000 and s30000 used a module linked with
  `--initial-memory=128 MiB`. Native builds of the same code for reference.
- Native: exact peak working set / peak private bytes, CPU and OS I/O of the
  process (handle, after exit); 3 repeats (2 for s1000). WASI: one fresh
  pinned node per run, Docker disabled, network `none`, read-only inputs,
  ready/ack protocol, every chunk hash-verified by the host before the ack;
  node lifetime peak minus idle baseline (includes ~89 MiB of module
  compilation for flpdf); 2 repeats, implementations interleaved. All runs on
  a warm OS cache; no cold-cache runs (they would disturb other processes).
- Times: `ready` = job start to the worker's ready marker; `all available` =
  ready + host copy and verification of every chunk (the ack); `first chunk
  usable` = first chunk copied and hash/map-verified by the host. Native
  `total` = process wall time (includes the worker's own verification).
- Safety: no OCR experiment or other Bacalhau/VIII process ran. Host RAM
  available swung between 0.6 and 3.1 GiB because of desktop apps. The guard
  killed 3 native 30k runs (pdq 2, flpdf 1), and admission refused 1 native
  pdq run and every pdq WASI run on s30000. All are recorded as such. None was
  retried with a weaker guard; native 30k completed later (3/3 each) when
  more memory was free.

## Results

### Bacalhau WASI, full document (median and range of 2 runs) [F]

| Scenario | Impl | ready | all verified | first chunk usable | node peak | node CPU | pages/s |
|---|---|---:|---:|---:|---:|---:|---:|
| s1000, 25 chunks | pdq | 1.5 | 1.9 | >= 1.5 (1.9 measured) | 122 | 1.5 | 527 |
| | flpdf | 5.1 | 5.6 | 2.6 | 97 | 5.0 | 178 |
| | flpdf lean | 5.0 | 5.5 | 2.5 | 97 | 5.0 | 181 |
| s10000, 250 | pdq | 8.0 (7.7-8.3) | 16.1 | >= 8.0 | 928 | 8.6 | 626 |
| | flpdf | 29.6 | 34.2 | 3.1 | 208 | 27 | 292 |
| | flpdf lean | 29.0 (26.2-31.9) | 34.4 | 3.7 | 187 | 26 | 293 |
| t30000, 750 | pdq | 15.3 (14.6-16.1) | 26.3 | >= 15.3 | **195** | 15 | 1,140 |
| | flpdf | 101.5 | 113.2 | 5.1 | 289 | 91 | 265 |
| | flpdf lean | 68.7 (66.7-70.7) | 80.9 | 5.8 | 248 | 60 | 371 |
| s30000, 750 | pdq | refused (2 x 3 runs) | | | | | |
| | pdq historical | 26.2 | 57.6 | >= 26.2 | 2,737 | 27 | 521 |
| | flpdf | 107.5 | 137.5 | 5.4 | 460 | 97 | 219 |
| | flpdf lean | 78.2 (76.0-80.3) | 100.8 | 5.6 | **424** | 66 | 298 |

pdq writes all chunks in one `pdq::split` call and has no per-chunk sidecar,
so a host can use its first chunk only after `ready` (runner measurement:
after all chunks are verified). Intermediate points (10 and 50 chunks) are in
`batch.json`. pdq's memory does not depend on the chunk count (927 MiB at
s10000 for 10, 50 or 250 chunks), flpdf's grows slowly (s10000: 141, 172,
208 MiB) **[F]**.

### Native, full document (median and range of 3 runs) [F]

| Scenario | Impl | total | peak private | peak working set | CPU | first chunk |
|---|---|---:|---:|---:|---:|---:|
| s1000 | pdq | 0.11 | 28 | 70 | 0.3 | 0.02 |
| | flpdf lean | 0.82 | 8 | 13 | 0.5 | 0.04 |
| s10000 | pdq | 3.2 (1.1-3.4) | 62 | 445 | 2.3 | 0.12 |
| | flpdf lean | 9.0 | 68 | 71 | 5.8 | 0.16 |
| t30000 | pdq | 2.2 | 91 | 103 | 5.6 | 0.23 |
| | flpdf lean | 26.9 (22.6-27.3) | 189 | 189 | 15.4 | 0.42 |
| s30000 | pdq | 6.3 (5.0-20.4) | 96 | **1,252** (mmap) | 9.8 | 0.24 |
| | flpdf lean | 31.7 (30.3-32.7) | 199 | 199 | 19.1 | 0.45 |

pdq's working set at s30000 is its mmap of the input (file-backed and
reclaimable, but it competes for RAM: with 1.9 GiB available it pushed the
host under the 1 GiB guard twice) **[F]**.

### Where flpdf's time goes (WASI, s30000, lean, median) [F]

Open 1.1 s, page tree 2.0 s, extraction 4.3 s, **resource pruning 36.4 s
(47% of `ready`)**, write 10.1 s, worker verification (re-open + count +
sha256) 17.8 s, module compilation ~2.5 s, rest ~4 s. Native: pruning 9.6 s,
verification 11.1 s, write 5.6 s, extraction 1.2 s. Pruning
(`remove_unreferenced_resources`, which parses each page's content
stream **[I]**) is CPU-bound and ~3.8x slower in wazero than natively.

### Reuse test: does flpdf amortize the page tree? [F]

- `extract_pages` reuses everything already parsed (objects, the cached page
  list) and reads **no** page-tree bytes again: index bytes are read once
  (`io.index_bytes_read`). Per-chunk extraction time is flat from the first
  to the last decile of the batch (s30000 native 10 ms -> 10-20 ms).
- But every call runs `push_inherited_attributes_to_pages`, an in-memory walk
  of the **whole** page tree (source: flpdf 0.10.0 `page_extract.rs` ->
  `optimization::inherited_attrs::push`). Timing one extra push per chunk:
  7.3 s for 750 chunks at t30000, ~80% of the extraction phase. So the
  batch costs one open + one parse + N in-memory page-tree walks, not N
  full re-parses.
- Minimal fix, public API only (`lean`, ~30 lines): the same steps as
  `extract_pages` (`Pdf::empty`, `copy_foreign_object` per page, `/Parent`,
  `/Kids`, `/Count`) with the push done once per batch; falls back to
  `extract_pages` if the source has `/PageLabels`. Every chunk is
  **byte-identical** to the library route (s1000 25/25, s10000 250/250,
  t30000 750/750, plus identical set digests at s30000). Extraction at
  s30000 in WASI: 34.5 s -> 4.3 s; `ready` 107.5 -> 78.2 s. No library change.
- A 2 KiB read buffer cuts bytes copied into the guest from 4.6 GB to 3.0 GB at
  s30000 and from 2.1 GB to 0.65 GB at t30000 (19 MB file). flpdf requests more
  bytes than the file holds because the `BufReader` refills on every object seek.
  These are page-cache hits on a warm cache. Their cost on a cold cache was not
  measured **[H]**.

### Memory over the batch [F]

- flpdf linear memory: s10000 default module 26 MiB after indexing -> 40 MiB
  after 250 chunks (~4.6 KiB per document page at the end). At 30k the
  128 MiB pre-sized module never grew. Extrapolation to 100k pages:
  ~450 MiB **[H]**.
- Node memory jumps within ~1 s of `ready` by ~160-180 MiB at s30000, ~16 MiB
  at t30000 and ~4 MiB at s10000, while guest linear memory is constant. It
  follows the output volume (1.19 GB vs 20 MB vs 0.4 GB). The flpdf s30000
  guest-phase peak is 242-300 MiB and the overall peak 421-461 MiB. Its cause in
  Bacalhau/wazero was not traced **[H]**. It applies to any batch with large
  results, pdq included.

### I/O and storage [F]

- Derivatives: 0.95-1.0x the original for both tools (s30000 1,191 vs
  1,193 MiB; t30000 18.1 vs 19.5 MiB). No chunk exceeded 4x the median bytes
  per page except chunks that hold scanned pages.
- Node OS reads at s30000: flpdf lean 4.7 GB (3.0 GB requested from the
  original plus re-reading every chunk for verification). pdq WASI s10000:
  1,193 MiB (the original plus its chunks). Native pdq reads via mmap (not
  counted). flpdf: 291,000 read calls on the original at 30k.

## Forensic integrity [F]

- **Reproducibility.** There is one set digest per (tool, fixture, chunk count)
  across all 154 runs, native and WASI, library and lean routes, buffer sizes
  and repeats. The flpdf batch chunk 29,961-30,000 is `29f0418e...` and pdq's
  is `5ade5f8b...`. Both are identical to the single-range chunks of PR #13.
- **Oracles.** qpdf 12.4.2 `--check` passed and `--show-npages` was correct on
  every chunk of s1000, s10000, t30000 and s30000 for both tools (3,550
  chunks). Poppler gives identical text and 30-dpi raster to the original's
  physical pages: all 1,000 pages of s1000 for both tools. For s10000,
  t30000 and s30000 the check covered the first chunk, both sides of the
  middle boundary, the last chunk and the chunk with page 29,999 (160 pages
  per set).
- **Across tools.** pdq and flpdf give the same raster for every common
  page (1,480/1,480). Their bytes differ, which is expected.
- **Mapping.** The runner verifies each chunk's exact `local -> physical`
  map and that chunks tile the range with no gap or overlap. pdq and flpdf
  produce the same boundaries and maps (test).
- **End to end.** The unchanged MVP OCR worker (`ed7b4f04`) ran on the flpdf
  chunk of pages 29,961-30,000 (`29f0418e`, the same bytes as the batch
  chunk). It found `JABUTICABA` on chunk-local page 39 and cited
  **physical page 29,999** of the original. 40/40 pages completed (35
  digital, 5 OCR). The OCR job peaked at 276 MiB and took 124 s. The
  original's sha256 was unchanged. A first attempt was killed by the host
  guard when other applications left 1.0 GiB available. It was rerun once
  memory was free, without changing the guard.
- **Original and logs.** The original's sha256 was unchanged after all 154
  runs. No fixture marker appeared in any job's stdout/stderr.
- **Adversarial cases, batch, 1,000 pages:**

| Case | pdq | flpdf |
|---|---|---|
| shared `/Resources` (all images) | correct, no bloat at 1,000 pages; **6.0x bloat** on the 12-page fixture (every 2-page chunk 1.27 MB) | correct, own images only at both sizes |
| object streams, incremental update | correct | correct |
| lying xref (+7) | correct, silent repair | correct, silent repair |
| truncated | correct; warning on stderr only (lost in Bacalhau) | correct; repair recorded in the manifest |
| AES-256 owner password only | **silently decrypted** | refused (worker policy) |
| AES-256 user password | fails cleanly | fails cleanly |

## Adaptive selection

Model (`selection_model.py`): `y = a + b*bytes + c*pages`, non-negative,
fitted on the full-document WASI runs and the labelled historical pdq run.
Fitted node memory: pdq 29 MiB + 2.17 x bytes; flpdf lean 80 MiB +
0.15 x bytes + 5.6 KiB x pages. Linear memory: pdq = file size; flpdf
~4.6 KiB x pages. Policy under test: pdq if it is safe (estimate + 512 MiB
margin <= available - reserved, linear < 4 GiB) and saves >= 30 s and >= 1.5x;
otherwise flpdf if safe; otherwise reject.

Replayed **in-sample** on the 4 measured documents x 5 available budgets
(1.5-8 GiB) x 2 reservations (0/1 GiB) = 40 cases:

| | completed | rejected (safety) | would exceed the budget | time, completed cases |
|---|---:|---:|---:|---:|
| fixed pdq | 34 | 0 | **6** (s30000 at 1.5-3 GiB, s10000 at 1.5 GiB - 1 GiB) | 716 s |
| fixed flpdf | **40** | 0 | 0 | 2,216 s |
| adaptive policy | 36 | 4 (budget 0.5 GiB) | 0 | 1,374 s |

Measured advantage per scenario: pdq is faster in 4/4 documents (1.75-3.1x,
3.6-54.5 s saved). Its advantage exceeds the policy's 30 s threshold in 2/4
(t30000, s30000). flpdf has the memory advantage in 3/4 (1.26x, 5.0x, 6.5x).
pdq has it in 1/4 (t30000, 1.27x).
The adaptive policy saves on average 17 s per document (at most 55 s) over
fixed flpdf in the 36 cases where both complete **[F, in-sample]**. That is under 0.3% of the OCR
time of the same documents (124 s per 40-page chunk) **[I]**.

If two strategies were ever kept (outcome B), the limits would be: pdq only
when `29 MiB + 2.2 x PDF bytes + margin <= node budget` **and** the
predicted saving is >= 30 s and >= 1.5x (in practice: large, light,
many-page documents on a node with free memory). Page count alone or size
alone would misclassify t30000 vs s30000.

## Decision

1. **Best absolute performance:** pdq native (s30000 in 6.3 s, ~4,800
   pages/s, 96 MiB private but a 1.25 GiB mmap working set) **[F]**. In WASI,
   pdq is still 1.75-4.5x faster than flpdf **[F]**.
2. **Most memory-efficient:** flpdf on real-evidence-like documents
   (424 vs 2,737 MiB at s30000, 187 vs 928 MiB at s10000). pdq wins only on
   many light pages (195 vs 248 MiB at t30000) **[F]**.
3. **Cost of WASI isolation:** pdq: 4.2x time to `ready` (6.3 -> 26.2 s) and
   from 96 MiB private to 2,737 MiB node peak. flpdf: 2.5x time (31.7 ->
   78.2 s) and from 199 MiB to 424 MiB **[F]**. Pruning runs ~3.8x slower in
   wazero than natively.
4. **Can flpdf batch compete with pdq batch?** On memory and time to first
   usable chunk, yes (5-6.5x less memory; 5.6 s vs >= 26 s). On total
   preparation time, no (1.75-3.1x slower to all-verified) **[F]**.
5. **Keep two algorithms?** No **[I]**. The measured gain is <= 55 s per
   30k document and is off the critical path once chunks are consumed as they
   are produced. The second path brings 2.7 GiB jobs, silent decryption, lost
   warnings and file-dependent resource bloat. Revisit only if preparation
   becomes the bottleneck. The next lever there is pruning/verification
   inside flpdf, not pdq.
6. **Limits that would govern a choice:** estimated node memory (bytes for
   pdq, pages and bytes for flpdf) + margin vs available minus reserved, the
   wasm32 4 GiB linear-memory ceiling (pdq holds the whole file; its node
   peak is ~2.2x the file, so ~1.6-1.8 GiB PDFs already need ~4 GiB **[I]**), and a minimum
   absolute saving (30 s) and ratio (1.5x). Note: page count or file size
   alone cannot predict both tools; the model needs both.
7. **Next blocker for the MVP:** the host side of progressive delivery. VIII
   must consume `chunk-*.json` sidecars, store chunks as operational
   derivatives bound to the evidence hash, and acknowledge the batch.
   Bacalhau deletes the result directory after the execution, so copying
   must happen while the worker waits. Then decide the encrypted-input
   policy and how to treat silent lying-xref repair.

**Conditions and open points:** pre-size flpdf's linear memory from the
largest accepted page count (128 MiB covers 30k; 100k needs a measurement,
~450 MiB **[H]**). Explain the node jump after `ready` on large results
**[H]**. Measure cold-cache I/O (flpdf requests 2.5-4x the file through the
page cache). Pin flpdf 0.10.0 (`38cd9b8e...`) and keep the lean route's
byte-identity check as an upgrade gate, or propose a per-document push cache
upstream. Run level 3 outside this workstation. The model was fitted and
replayed on the same 4 documents.

## Reproduction

Tools as in [ALTERNATIVES.md](ALTERNATIVES.md) (Python 3.15 + psutil, Rust
with `wasm32-wasip1`, qpdf 12.4.2 and Poppler 25.07 on `PATH`, pinned
Bacalhau). Fixtures as in README.md; t30000 as in ALTERNATIVES.md.

```bash
R=.poc-runtime/batch
(cd experiments/pdf-chunker/workers/pdq-wasi && python prepare.py && cargo build --release --locked \
   && cargo build --release --locked --target wasm32-wasip1)
(cd experiments/pdf-chunker/workers/flpdf-wasi && cargo test --release && cargo build --release \
   && cargo build --release --target wasm32-wasip1 \
   && RUSTFLAGS="-C link-arg=--initial-memory=134217728" cargo build --release --target wasm32-wasip1 --target-dir target-initmem)
# copy: pdq-native.exe, pdq.wasm, flpdf-native.exe, flpdf.wasm, flpdf-im128.wasm into $R
cd experiments/pdf-chunker/harness   # harness scripts; R is relative to the repository root
FLPDF_CHUNKER=$R/flpdf-native.exe PDQ_CHUNKER=$R/pdq-native.exe python -m unittest test_batch test_bench
LEAN="flpdf-lean=FLPDF_EXTRACT=lean,CHUNKER_READ_BUF=2048"
python batch_bench.py native --impl pdq=$R/pdq-native.exe flpdf=$R/flpdf-native.exe flpdf-lean=$R/flpdf-native.exe \
  --env $LEAN --fixture s1000=... s10000=... t30000=... s30000=... --repeat 3 --out $R/native
python batch_bench.py wasi --impl flpdf=$R/flpdf.wasm flpdf-lean=$R/flpdf.wasm pdq=$R/pdq.wasm --env $LEAN \
  --fixture s10000=... --repeat 2 --memory 2gb --out $R/wasi-s10000
python batch_bench.py wasi --impl flpdf=$R/flpdf-im128.wasm flpdf-lean=$R/flpdf-im128.wasm pdq=$R/pdq.wasm --env $LEAN \
  --fixture s1000=... t30000=... s30000=... --repeat 2 --memory 1gb --out $R/wasi-main
python batch_validate.py --original s1000.pdf --set pdq=<chunk dir> flpdf=<chunk dir> --all --out $R/validation/s1000.json
python batch_validate.py --original mixed-30000.pdf --set pdq=... flpdf=... --sample first,boundary,last,target:29999 --out ...
python -c "import adversarial,pathlib; adversarial.make(pathlib.Path('s1000.pdf'), pathlib.Path('$R/adv1000'))"
python batch_adversarial.py --fixtures $R/adv1000 --pdq $R/pdq-native.exe --flpdf $R/flpdf-native.exe --out $R/adv-batch
python e2e_ocr.py --original mixed-30000.pdf --first 29961 --last 30000 --chunker $R/flpdf-im128.wasm \
  --worker <MVP>/pdf-worker.wasm --model-dir <MVP>/models --expect-page 29999 --results $R/e2e --memory 1gb --min-available-mb 1024
python batch_summarize.py --runs $R/native* $R/wasi-* --fixtures fixtures.json --historical historical.json \
  --extra extra.json --out ../results/batch.json
```

`fixtures.json`, `historical.json` and `extra.json` (identities, validation,
adversarial and e2e summaries) are embedded in `results/batch.json`.
