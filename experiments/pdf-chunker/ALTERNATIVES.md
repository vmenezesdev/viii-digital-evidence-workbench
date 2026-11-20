# PDF chunking in WASI with on-demand I/O: alternatives to pdq

Branch `experiment/pdf-chunker-io-alternatives` (from `experiment/pdf-chunker-wasi`,
PR #13, issue #14). Measured on 2026-11-20 on the same Windows workstation,
fixtures and runner as [RESULTS.md](RESULTS.md). Every number comes from
[`results/alternatives.json`](results/alternatives.json) (built by
`alt_summarize.py` from the raw runs) unless it is quoted from RESULTS.md,
which is marked. Units: MiB/GiB. Labels: **[F]** observed fact, **[I]**
inference from observations, **[H]** hypothesis not demonstrated.

## Answer

**Yes: flpdf 0.10.0** (Rust, MIT OR Apache-2.0) extracts a page range from a
1.2 GiB PDF inside a WASI module through `Read + Seek`, without reading the
file into memory, and it runs on the pinned Bacalhau v1.9.5 / wazero v1.12.0
**with no change to the library** **[F]**. The worker is ~300 lines of
application code over flpdf's public API (`Pdf::open`, `extract_pages`,
`PageDocumentHelper::remove_unreferenced_resources`, `PdfWriter`).

Decisive measurement **[F]**, physical pages 29,961-30,000 of `mixed-30000.pdf`
(1,193 MiB, sha256 `eb3ca405...`) as a Bacalhau job:

| | node peak minus idle baseline | job until `ready` | guest linear memory |
|---|---:|---:|---:|
| pdq + WASI read patch (RESULTS.md) | 2,719 MiB | 4.6-7.3 s | (whole file) |
| **flpdf**, default module | 390 MiB (389-391) | 6.3 s | 75 MiB |
| **flpdf**, module linked with `--initial-memory=96 MiB` | **161 MiB (160-163)** | 5.3 s (4.3-6.4) | 96 MiB |

The 161 MiB includes ~89 MiB that wazero spends compiling the 4.9 MB module
before any PDF work (startup control below). The chunk is
text- and raster-identical to the original pages (Poppler), passes
`qpdf --check`, and has the same sha256 (`29f0418e...`) natively, under
Wasmtime and under Bacalhau, in every repeat. The original's sha256 was
unchanged after every run.

The experimental target (< 256 MiB additional on the WASI node at ~1.2 GiB,
not linear in file size) is met with the pre-sized module. One qualification
**[F]**: flpdf's memory is not constant either. It grows with the **page
count** (about 2.5 KiB of linear memory per page of the document), not with
the file size. See [Memory model](#what-memory-grows-with).

Classification: **A, usable existing library**, with the conditions listed in
[Decision](#decision).

## Candidates and triage

Versions as of 2026-11-20. Evidence type per answer: **doc** (documentation),
**code** (source inspection), **exp** (this experiment).

| Question | flpdf 0.10.0 | otf-pdf 0.2.0 | pdf_oxide 0.3.78 | pdf-rs (`pdf`) 0.10.0 | pdq 0.5.1 (baseline) |
|---|---|---|---|---|---|
| Page-range extraction to a valid PDF | yes, `extract_pages` + `PdfWriter` (exp) | yes, `Document::extract_pages` + `save_to` (exp) | via editor; not tried | no first-class API (code) | yes (RESULTS.md) |
| Random access to the input | `Read + Seek` (code, exp) | `Source::read_at` trait, caller-implemented (code, exp) | `Document::open` = `std::fs::read` of the whole file (code) | `Backend::read` returns `&[u8]` borrowed from contiguous bytes (code) | `&[u8]` over mmap; `Vec<u8>` in WASI (code) |
| Works without loading the whole PDF | **yes** (exp: 477 MiB requested for a 1,193 MiB file, 75 MiB linear memory) | **no, in practice**: a large classic xref table is rejected and the whole file is scanned (exp, below) | no (code) | no (code) | no in WASI (RESULTS.md) |
| Compiles to `wasm32-wasip1` | yes, unmodified (exp) | yes, unmodified (exp) | not tried | not tried | yes with local patch |
| Threads / OS / Wasm EH | none needed at runtime (exp); `stacker`/`psm`, `libc`, `getrandom` compile for wasip1 | `Mutex`/`OnceLock` only (code) | - | - | - |
| Materializes the object tree or file-sized buffers | no file-sized buffer; page dictionaries of the **whole** page tree are loaded (exp, code) | object cache grows with objects read; whole-file scan on fallback (code, exp) | yes (code) | yes (code) | whole file in WASI |
| License / maturity | MIT OR Apache-2.0; 0.x, qpdf port by one main author, last commit 2026-10-11 | Apache-2.0; 0.2.0 released 2026-11-03, very young | MIT OR Apache-2.0 | MIT | Artistic-2.0 |
| Integration effort | worker of ~300 lines, same contract (exp) | same worker plus a 40-line `Source` (exp), **plus an upstream xref fix** | rewrite of its I/O layer | rewrite of its I/O layer | see C below |

pdf_oxide and pdf-rs were discarded at triage: both require the input as one
contiguous byte slice, which in WASI P1 means reading the whole file into
linear memory: the same defect as the current pdq patch. Neither was built.

## Implementations (code in this branch)

| Path | What |
|---|---|
| `flpdf-wasi/` | flpdf 0.10.0 chunker. `Read + Seek` over `File` via an 8 KiB `BufReader` and a counting wrapper that keeps the position in the guest, so a seek to the current position does not call the host. Prunes unreferenced `/Font` and `/XObject` resources (qpdf's `--pages` default). Deterministic `/ID`. Refuses encrypted input. |
| `otfpdf-wasi/` | otf-pdf 0.2.0 chunker. `Source::read_at` over `File` (seek + read under a mutex). Same contract and manifest. |
| `alt_bench.py` | Native and Wasmtime runs with exact OS peaks (reuses `bench.run_one`), a host memory guard, a timeout, and an sha256 check of the original before and after. |
| `alt_summarize.py` | Builds `results/alternatives.json` from the raw runs. |
| `bench.py`, `adversarial.py` | Small additions: an `flpdf` CLI entry, and `FLPDF_CHUNKER` plus manifest diagnostics in the adversarial run. |

Both chunkers accept the requested simple contract
`chunker original.pdf first last output.pdf` (prints one JSON line of
metrics, never content) and the VIII worker contract of `pdq-wasi`
(`... out-dir ack-file [sha256]`, manifest `viii.pdf-chunk.v1`, `ready` last,
explicit exit, single-write errors). That second contract let `bacalhau_wasi.py`,
`run_matrix.py`, `validate.py` and `adversarial.py` be reused unchanged for
level 3. Each manifest adds `io` (bytes, read calls and host seeks the guest
made on the original), `guest_linear_memory_final` (wasm32 `memory.size`,
which never shrinks, so it is the guest's peak), phase timings and
`diagnostics` (repair warnings).

## Compatibility levels [F]

| Candidate | 1. compiles (wasm32-wasip1) | 2. Wasmtime 51.0.0 | 3. pinned Bacalhau v1.9.5 |
|---|---|---|---|
| flpdf | yes, 4.9 MB module | yes, s12 -> s30000 | **yes, s12 -> s30000**, 48 jobs (p1, p32, late40 and t30000; 2 modules; 2 repeats), all exit 0, all verified |
| otf-pdf | yes, 0.75 MB module | yes, s12 -> s10000 | yes, s12, s10000, s30000 late40 (1 run each), exit 0, verified |

## Measurements

Same method as RESULTS.md. Native and Wasmtime: exact peak working set and
peak private bytes of the process, read from its handle after exit; median of
3. Bacalhau: node lifetime peak minus the node's idle RSS before submission
(so it includes wazero compilation); median and range of 2 runs, each on a
fresh node with Docker disabled, network `none`, read-only input and a 1 GiB
job memory limit. Ranges: `late40` = last 40 pages, `p1` = page N-1, `p32` =
32 pages in the middle. All on a warm OS file cache.

### flpdf, late40

| Fixture (pages / file) | native peak WS / private | native time | Wasmtime peak WS | Wasmtime time | guest linear memory | guest bytes requested from the original |
|---|---|---|---|---|---|---|
| s12 (12 / 1.2 MiB), pages 9-12 | 6.6 / 1.7 MiB | 0.03 s | 38.5 MiB | 0.08 s | 3.3 MiB | 2.0 MiB |
| s1000 (1,000 / 40 MiB) | 10.8 / 6.1 MiB | 0.06 s | 38.5 MiB | 0.10 s | 4.9 MiB | 22 MiB |
| s10000 (10,000 / 398 MiB) | 43.0 / 38.9 MiB | 0.20 s | 50.2 MiB | 0.30 s | 26.4 MiB | 163 MiB |
| s30000 (30,000 / 1,193 MiB) | 113.9 / 112.2 MiB | 0.56 s | 98.0 MiB | 0.71 s | 74.9 MiB | 477 MiB |
| t30000 (30,000 / 19.4 MiB, text only) | 111.0 / 109.0 MiB | 0.64 s | 96.3 MiB | 0.76 s | 73.2 MiB | 473 MiB |

Same module under Bacalhau (node peak minus baseline; ready = job start to
`ready`, including ~2.5 s of module compilation):

| Fixture, range | default module | ready | `--initial-memory=96 MiB` module | ready |
|---|---|---|---|---|
| startup control (exits at once) | 89 MiB (86-90) | 2.6 s | 89 MiB (88-91) | 2.5 s |
| s12 p1 / p32 | 96 / 99 MiB | 2.3 / 2.4 s | 90 / 93 MiB | 2.4 / 2.5 s |
| s1000 late40 | 103 MiB (98-109) | 2.6 s | 91 MiB (89-93) | 2.7 s |
| s10000 late40 | 154 MiB (138-169) | 4.0 s | 113 MiB (112-115) | 4.0 s |
| s30000 p1 / p32 / late40 | 347 / 312 / 390 MiB (271-391) | 6.2-6.9 s | **165 / 163 / 161 MiB** (160-166) | 5.3-6.4 s |
| t30000 late40 | 298 MiB (282-314) | 5.8 s | 161 MiB (160-162) | 5.7 s |

For reference (RESULTS.md, same machine and day): pdq-WASI s10000 928 MiB,
s30000 2,719 MiB, startup control 29 MiB / 0.6-0.8 s; Wasmtime s30000
1,215 MiB; native pdq s30000 29-46 MiB / 0.14-0.23 s. In this experiment, the
recorded pdq module (sha256 `31b06bff...`) on t30000 under Bacalhau: 108 MiB,
2.6-3.3 s.

Chunk sizes at 30k: late40 2.04 MiB, p32 1.19 MiB, p1 0.40 MiB. Module:
4,985,423 bytes.

### What memory grows with

- **flpdf: page count, not file size [F].** s30000 (1,193 MiB) and t30000
  (19.4 MiB) both have 30,000 pages. They need the same linear memory
  (74.9 vs 73.2 MiB) and the same bytes requested (477 vs 473 MiB). The
  position of the range does not matter (p1, p32 and late40 at 30k all reach
  74.9 MiB). Code inspection explains it: `extract_pages` calls
  `get_all_pages` and `push_inherited_attributes_to_pages`, which load every
  page dictionary of the document (qpdf semantics). Linear memory is about
  2.5 KiB per page **[I]**, so a 100,000-page document would need ~250 MiB of
  linear memory **[H, not tested]**.
- **pdq-WASI: file size [F]** (RESULTS.md: whole file in a `Vec`). So pdq
  wins on documents with many light pages (t30000: 108 vs 161 MiB), and flpdf
  wins by an order of magnitude on heavy, scanned documents like real
  evidence (s30000: 161 vs 2,719 MiB).
- **otf-pdf: file size in practice [F]**, see below.

### Where the Bacalhau overhead comes from [F]

1. **Module compilation**: ~89 MiB and ~2.5 s per job for the 4.9 MB flpdf
   module (startup control). This is paid per job, not per page.
2. **Linear-memory growth in wazero**: with the default module, the 30k node
   peak is 312-391 MiB for 75 MiB of linear memory. Linking the same code
   with a 96 MiB initial memory (no growth at runtime) drops it to
   160-166 MiB with byte-identical output. This confirms the effect behind
   RESULTS.md's open **[H]** ("node peak ≈2.3x linear memory"): growing linear
   memory costs wazero transient memory far beyond the memory itself. The
   mechanism inside wazero (reallocate-and-copy on `memory.grow` plus Go GC
   headroom) is **[I]** and was not traced.
3. **Host calls**: the job takes 2.7-4.0 s of guest time in wazero vs 0.7 s
   in Wasmtime for the same 60,379 reads and 90,419 host seeks (manifest
   counters).

### I/O [F]

- The guest requested 477 MiB from a 1,193 MiB file (40%), and 473 MiB from a
  19.4 MiB file. Bytes requested follow the **number of object reads**, about
  2 per page of the document, times the 8 KiB `BufReader` refill. They do not
  follow the file size. With a 1 KiB buffer, s1000 needed 6.8 MiB instead of
  22 MiB at the same speed (interim native run). Most of these bytes are OS
  file-cache hits copied into the guest. They are not disk reads (warm cache;
  cold cache not measured).
- OS-level `read_bytes` for the native process matches the guest counter
  (479 vs 477 MiB at s30000). Native page faults at s30000: 36,332. There is
  no mmap, so `read_bytes` is real here, unlike pdq native.

### otf-pdf: why it fails the I/O criterion [F]

otf-pdf's design is on-demand (`Source::read_at`, 4 KiB parse windows, lazy
object cache). On the fixtures it nevertheless requested **439 MiB of a
398 MiB file** (s10000) and **1,313 MiB of 1,193 MiB** (s30000). Bacalhau:
208 MiB / 16.5 s at 10k and 487 MiB / 53 s at 30k, with 131 MiB of linear
memory. Tracing the reads shows sequential 1 MiB reads from offset 0: a
whole-file scan. otf-pdf reports `bad xref section`, `bad startxref`,
`xref rebuilt by scanning` for files that qpdf `--check`, Poppler, flpdf and
pdq read without repair. The same s1000 content rewritten by qpdf behaves
differently by layout:

| s1000 rewritten by qpdf | bytes otf-pdf requested | otf-pdf diagnostics |
|---|---:|---|
| classic xref table (`--object-streams=disable`, 2,105 entries) | 46 MiB | xref rebuilt by scanning |
| xref stream (`--object-streams=generate`) | 2 MiB | none |

So otf-pdf 0.2.0 cannot load a large classic cross-reference table, and it
falls back to scanning the whole file. s12 (32 entries) loads fine. Its
output was still correct (30k late40 identical in text and raster), but it
records **false repair diagnostics** for intact evidence, which a forensic
manifest must not do. Unblocking it means an upstream fix in
`otf-pdf-syntax`'s xref-section loader **[I: likely small, not attempted]**,
on a crate released 17 days ago.

## Forensic validation (flpdf, final module) [F]

- **Content**: every chunk compared page by page with the original's
  physical pages using `validate.py` (Poppler 25.07 `pdftotext -layout` and
  `pdftoppm -gray -r 30`, byte-identical). Covered: s12 p1 and p32, s1000,
  s10000 and s30000 late40, s30000 p1 (scanned page 29,999 with
  `JABUTICABA`) and p32, and t30000 late40. All pages identical in text and
  raster, including the scanned pages. All pass `qpdf --check`, and the
  `qpdf --show-npages` count equals the range length.
- **Local -> physical mapping**: the manifest's map is verified by the runner.
  Page identity was proven by the per-physical-page comparison above.
- **Original unchanged**: sha256 before and after every native, Wasmtime and
  Bacalhau run (`original_unchanged`, `mounts_unchanged`).
- **Reproducibility**: each range yields one sha256 across all repeats, across
  native/Wasmtime/Bacalhau, and across the default and pre-sized modules
  (e.g. late40 at 30k `29f0418e...` in 10 runs). The writer uses
  `set_deterministic_id(true)`.
- **No content leaks** in job stdout/stderr (leak scan).

Adversarial cases (pages 2-3; Poppler as oracle; RESULTS.md method):

| Case | flpdf native | flpdf WASI/Bacalhau | otf-pdf WASI/Bacalhau | pdq (RESULTS.md) |
|---|---|---|---|---|
| shared `/Resources` with every image | correct, **1.7 KiB** | correct, **1.7 KiB** | correct, 1,242 KiB | correct, 1,242 KiB |
| object streams + xref stream | correct | correct | correct | correct |
| incremental update | correct (latest revision) | correct | correct | correct |
| lying xref (offsets +7) | correct, **silent repair** | correct, **silent repair** (no diagnostic) | correct, silent repair | correct, silent repair |
| truncated (no xref/trailer) | correct, warning on stderr | correct, **repair recorded in the manifest** | correct, repair recorded | correct, warning lost in Bacalhau |
| AES-256, owner password only | refused (worker policy) | refused (worker policy) | refused (worker policy) | silently decrypted |
| AES-256, user password | fails cleanly | fails cleanly | fails cleanly | fails cleanly |

Without the `remove_unreferenced_resources` call, flpdf also produced
1,242 KiB for the shared-resource case (interim run), like pdq. qpdf's
`--pages` prunes by default; the flpdf library function does not. Repairs of
a lying xref remain undetectable through flpdf's diagnostics, the same as qpdf.
Encrypted input is refused by the worker, not by the library: the policy is
still undecided.

## Alternative C: pdq with on-demand I/O (estimate, not implemented)

Replacing pdq's `Vec<u8>` with a `ReadAt` source is not a one-function patch
**[code]**. The input is a borrowed `&'a [u8]` threaded through `PdfSource`,
`LazyPdf`, `xrefboot` (1,108 lines), `repair` (884), `split`,
`split_template`, `merge`, `write` and `copy` (raw byte-copy of object spans).
That is ~180 buffer uses in 11 files. lopdf's parser and pdq's eager and
repair fallbacks (`Document::load_mem`, `reconstruct_document`) need the whole
buffer by design, so only the lazy fast path could become on-demand.
Estimate **[H]**: several days of work, plus a large fork of a 0.x crate to
maintain, to get what flpdf does unmodified. Not worth doing while A holds.

## Decision matrix

| Criterion | pdq native | pdq WASI (current) | **flpdf WASI** | otf-pdf WASI | pdf_oxide / pdf-rs |
|---|---|---|---|---|---|
| Bacalhau compatibility | outside Bacalhau | level 3 | **level 3** (48 jobs) | level 3 (3 jobs) | not tested (discarded) |
| Peak memory, 30k late40 | 29-46 MiB | 2,719 MiB | **161 MiB** pre-sized / 390 MiB default | 487 MiB | - |
| Extraction time, 30k | 0.14-0.23 s | 4.6-7.3 s | 5.3-6.4 s ready (~2.5 s compile) | 53 s | - |
| On-demand reading | mmap | no (whole file) | **yes** (40% of bytes requested, page-count bound) | no (whole-file scan on classic xref) | no |
| Forensic correctness | identical | identical | **identical**; resources pruned; repairs recorded except lying xref | identical; false repair diagnostics | - |
| Reproducibility | yes | yes | **yes** (native = Wasmtime = Bacalhau) | yes (s12, native = Wasmtime) | - |
| Dependencies / maintenance | lopdf | local patch | crates.io, unmodified; 0.x, one main author | 0.2.0, 17 days old | - |
| Integration effort | new execution path | done | worker ready; batch mode not written | upstream fix needed | rewrite I/O |
| Class | - | C baseline | **A** | B | D-equivalent |

## Decision

**Recommendation [I]: replace the pdq-WASI chunker with flpdf 0.10.0 (class
A), linked with a pre-sized initial memory**, conditional on the points below.
It meets criteria 1-3 in this order: correct and verified output, demonstrated
level 3, and memory independent of the file size at 161 MiB for the 1.2 GiB
document. It also removes the local pdq patch, records repair diagnostics in
the manifest, and fixes the shared-resource bloat.

Conditions and open points:

1. **Page-count scaling.** About 2.5 KiB of linear memory per page of the
   document. Choose `--initial-memory` from the largest page count accepted:
   96 MiB covers 30k pages. Measure a 100k-page fixture before accepting such
   documents **[H: ~250 MiB]**.
2. **Batch mode is not implemented.** RESULTS.md recommends one preparation
   job per document. At ~5-6 s per range job, 750 jobs for 30k pages would
   take over an hour serially, against pdq's 26 s batch. Opening once and
   extracting many ranges uses the same API in a loop. Its memory with many
   outputs is **[H]** and must be measured before integration.
3. **Maintenance risk.** flpdf is 0.x with essentially one author. Pin the
   exact version and checksum (`38cd9b8e...` in `Cargo.lock`). Keep the
   adversarial suite as the upgrade gate.
4. **Not measured**: cold OS cache, concurrent flpdf jobs, OCR end-to-end on
   flpdf chunks (expected identical because chunks are text- and
   raster-identical **[I]**), large shared-resource documents, PDFs whose
   page tree is deep or broken.
5. **Policy, unchanged from RESULTS.md**: encrypted inputs (refused here) and
   silent lying-xref repairs (not detectable through flpdf diagnostics).

Optional tuning, not needed for the target: a 1-2 KiB read buffer cuts the
bytes copied into the guest about 3x **[F, native s1000]**.

## Reproduction

Tools: those of RESULTS.md, plus Wasmtime 51.0.0 (zip sha256 `d2f5853a...`)
and qpdf 12.4.2 msvc64 (zip sha256 `db87077e...`). Rust 1.99.1 with
`wasm32-wasip1`. No network access is needed at run time.

```bash
R=.poc-runtime/alt; F=.poc-runtime/pdf-chunker/fixtures   # fixtures as in README.md
F30=<MVP>/.poc-runtime/pdf/fixtures/mixed-30000.pdf          # read-only, sha256 eb3ca405...
python scripts/make-pdf-fixture.py $R/fixtures/t30000.pdf --pages 30000 --ocr-every 1000000

cd experiments/pdf-chunker/flpdf-wasi && cargo test --release && cargo build --release
cargo build --release --target wasm32-wasip1
RUSTFLAGS="-C link-arg=--initial-memory=100663296" cargo build --release --target wasm32-wasip1 --target-dir target-initmem
cd ../otfpdf-wasi && cargo build --release && cargo build --release --target wasm32-wasip1
# copy the modules to $R/flpdf.wasm, $R/flpdf-im96.wasm, $R/otfpdf.wasm

# native and level 2 (repeat per fixture/range)
python experiments/pdf-chunker/alt_bench.py --chunker <native exe> --name flpdf \
  --fixture s30000=$F30 --range 29961-30000 --repeat 3 --out $R/n-flpdf-s30000 --log $R/native.jsonl
python experiments/pdf-chunker/alt_bench.py --wasmtime <wasmtime.exe> --max-linear-mib 512 --chunker $R/flpdf.wasm \
  --name flpdf --fixture s30000=$F30 --range 29961-30000 --repeat 3 --out $R/w-flpdf-s30000 --log $R/wasmtime.jsonl

# level 3 (existing runner and protocol)
python experiments/pdf-chunker/run_matrix.py wasi --fixture s12=$F/s12.pdf s1000=$F/s1000.pdf s10000=$F/s10000.pdf \
  --module flpdf=$R/flpdf.wasm flpdf-im96=$R/flpdf-im96.wasm --out $R/matrix --repeat 2 --memory 1gb --min-available-mb 1024
python experiments/pdf-chunker/run_matrix.py wasi --fixture s30000=$F30 \
  --module flpdf=$R/flpdf.wasm flpdf-im96=$R/flpdf-im96.wasm --out $R/matrix --repeat 2 --memory 1gb --min-available-mb 1024
python experiments/pdf-chunker/run_matrix.py startup --module flpdf=$R/flpdf.wasm flpdf-im96=$R/flpdf-im96.wasm --out $R/matrix --repeat 3 --memory 1gb
# t30000 and otf-pdf runs: bacalhau_wasi.py --plan <plan> with the same plan shape (worker contract args)

# forensic checks
python experiments/pdf-chunker/validate.py --original $F30 --derived <chunk.pdf> --first 29961 --last 30000 --output $R/validate/s30000-late40.json
FLPDF_CHUNKER=<native exe> python experiments/pdf-chunker/adversarial.py run --fixtures $F/adversarial \
  --results $R/adversarial --wasm flpdf=$R/flpdf.wasm
python experiments/pdf-chunker/alt_summarize.py --runs $R --out experiments/pdf-chunker/results/alternatives.json
```

Safety: no MVP OCR job or other `bacalhau.exe`/`VIII.exe` process ran during
the measurements. Every run had a host memory guard (1 GiB available
minimum), a timeout and a 1 GiB Bacalhau job memory limit. The 30k runs
started only after s10000 stayed under 170 MiB. The guard never tripped. The
pdq 30k run was not repeated here (2.7 GiB on a host with ~2.8 GiB
available); its figures are quoted from RESULTS.md.
