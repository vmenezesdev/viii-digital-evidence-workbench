//! VIII PDF chunker (flpdf 0.10.0 backend), selected by ADR-0003.
//! Copied from experiments/pdf-chunker/flpdf-wasi (kept there as the measured
//! record). Product defaults: lean extraction route and 2 KiB read buffer
//! (BATCH-RESULTS.md); FLPDF_EXTRACT=library and CHUNKER_READ_BUF override them.
//!
//! Two contracts:
//!
//!   chunker original.pdf first last output.pdf
//!   chunker original.pdf first last out-dir ack-file [declared-source-sha256]
//!   chunker original.pdf first last out-dir ack-file source-sha256 chunk-size
//!   chunker original.pdf --chunk-size N --out-dir DIR [--range A-B] [--ack FILE] [--source-sha256 S]
//!
//! The last two are the batch mode (BATCH-RESULTS.md): the original is opened
//! ONCE and [first,last] is split into consecutive chunks of at most N pages,
//! written as out-dir/chunk-AAAAAA-BBBBBB.pdf, each followed by a sidecar
//! chunk-AAAAAA-BBBBBB.json (its hash and page map, so a host may verify and
//! use it before the batch ends), then manifest.json (`viii.pdf-chunk-set.v1`,
//! the schema of pdq-wasi's batch) and `ready`. The 7-argument positional form
//! is pdq-wasi's batch contract, so the existing runner verifies both alike.
//! Without --ack the flag form exits after writing (native measurements).
//!
//! The second is the VIII worker contract shared with pdq-wasi and pdfcpu-go:
//! it writes out-dir/chunk.pdf, manifest.json, then `ready` (content "ready")
//! and waits for the host acknowledgement. It never stats inside out-dir
//! (Bacalhau v1.9.5's /out creates files on stat), never renames, writes final
//! names directly and exits explicitly. Never prints PDF content.
//!
//! The original is opened through `Read + Seek` on a plain `File` (WASI
//! `fd_seek` + `fd_read`); nothing reads it to the end. A counting wrapper
//! records the bytes the guest actually pulled from the file system, and on
//! wasm32 the final linear-memory size (which never shrinks) is recorded as the
//! guest's peak linear memory.
use flpdf::{extract_pages, ObjectHandle, ObjectRef, PageDocumentHelper, Pdf, PdfWriter};
use sha2::{Digest, Sha256};
use std::{
    cell::Cell,
    fs,
    io::{BufReader, Read, Seek, SeekFrom, Write},
    path::{Path, PathBuf},
    rc::Rc,
    thread,
    time::{Duration, Instant},
};

const MAX_PAGES: usize = 30000;
const MAX_CHUNK_PAGES: usize = 1000;
const FLPDF_VERSION: &str = "0.10.0";

#[derive(Default)]
struct IoStats {
    bytes: Cell<u64>,
    reads: Cell<u64>,
    seeks: Cell<u64>,
}

/// `File` wrapper that counts what is actually requested from the host and
/// keeps the file position in the guest: a seek to the current position (what
/// `BufReader::stream_position` and most parser seeks amount to) costs no host
/// call. `seeks` counts only the seeks that reached the host.
struct Counting {
    file: fs::File,
    pos: u64,
    stats: Rc<IoStats>,
}

impl Read for Counting {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        let n = self.file.read(buf)?;
        self.pos += n as u64;
        self.stats.bytes.set(self.stats.bytes.get() + n as u64);
        self.stats.reads.set(self.stats.reads.get() + 1);
        Ok(n)
    }
}

impl Seek for Counting {
    fn seek(&mut self, pos: SeekFrom) -> std::io::Result<u64> {
        let target = match pos {
            SeekFrom::Start(n) => Some(n),
            SeekFrom::Current(d) => self.pos.checked_add_signed(d),
            SeekFrom::End(_) => None,
        };
        if target == Some(self.pos) {
            return Ok(self.pos);
        }
        self.stats.seeks.set(self.stats.seeks.get() + 1);
        self.pos = self.file.seek(pos)?;
        Ok(self.pos)
    }
}

fn linear_memory_bytes() -> Option<u64> {
    #[cfg(target_arch = "wasm32")]
    {
        Some(core::arch::wasm32::memory_size(0) as u64 * 65536)
    }
    #[cfg(not(target_arch = "wasm32"))]
    {
        None
    }
}

#[derive(Debug, PartialEq)]
struct Request {
    input: PathBuf,
    first: usize,
    last: usize,
    /// Simple contract: write exactly this file and exit.
    output: Option<PathBuf>,
    out: PathBuf,
    ack: PathBuf,
    declared_sha: Option<String>,
    /// Batch mode: chunk size in pages; `last == 0` means "to the end".
    chunk_size: Option<usize>,
    /// Batch mode: wait for `ack` after `ready` (Bacalhau protocol).
    wait_for_ack: bool,
}

fn valid_sha(s: &str) -> bool {
    s.len() == 64 && s.bytes().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
}

fn parse_chunk_size(v: &str) -> Result<usize, String> {
    match v.parse::<usize>() {
        Ok(n) if (1..=MAX_CHUNK_PAGES).contains(&n) => Ok(n),
        _ => Err(format!("invalid chunk size (1..={MAX_CHUNK_PAGES})")),
    }
}

/// Flag form of the batch contract.
fn parse_flags(args: &[String]) -> Result<Request, String> {
    let mut r = Request {
        input: args[0].clone().into(),
        first: 1,
        last: 0,
        output: None,
        out: PathBuf::new(),
        ack: PathBuf::new(),
        declared_sha: None,
        chunk_size: None,
        wait_for_ack: false,
    };
    let mut it = args[1..].iter();
    while let Some(flag) = it.next() {
        let value = it.next().ok_or_else(|| format!("{flag} needs a value"))?;
        match flag.as_str() {
            "--chunk-size" => r.chunk_size = Some(parse_chunk_size(value)?),
            "--out-dir" => r.out = value.into(),
            "--ack" => {
                r.ack = value.into();
                r.wait_for_ack = true;
            }
            "--source-sha256" if valid_sha(value) => r.declared_sha = Some(value.clone()),
            "--source-sha256" => return Err("declared source sha256 must be 64 lowercase hex digits".into()),
            "--range" => {
                let (a, b) = value.split_once('-').ok_or("range must be A-B")?;
                r.first = a.parse().map_err(|_| "invalid first page")?;
                r.last = b.parse().map_err(|_| "invalid last page")?;
                if r.first == 0 || r.last < r.first || r.last > MAX_PAGES {
                    return Err("invalid physical page range".into());
                }
            }
            other => return Err(format!("unknown option {other}")),
        }
    }
    if r.chunk_size.is_none() || r.out.as_os_str().is_empty() {
        return Err("batch mode needs --chunk-size and --out-dir".into());
    }
    Ok(r)
}

fn parse_args(args: &[String]) -> Result<Request, String> {
    if args.len() >= 2 && args[1].starts_with("--") {
        return parse_flags(args);
    }
    if !(4..=7).contains(&args.len()) {
        return Err("usage: chunker original.pdf first last output.pdf | chunker original.pdf first last out-dir ack-file [source-sha256]".into());
    }
    let first: usize = args[1].parse().map_err(|_| "invalid first page")?;
    let last: usize = args[2].parse().map_err(|_| "invalid last page")?;
    if first == 0 || last < first || last > MAX_PAGES {
        return Err("invalid physical page range".into());
    }
    let declared_sha = match args.get(5) {
        Some(s) if valid_sha(s) => Some(s.clone()),
        Some(_) => return Err("declared source sha256 must be 64 lowercase hex digits".into()),
        None => None,
    };
    let simple = args.len() == 4;
    let chunk_size = args.get(6).map(|v| parse_chunk_size(v)).transpose()?;
    Ok(Request {
        input: args[0].clone().into(),
        first,
        last,
        output: simple.then(|| args[3].clone().into()),
        out: args[3].clone().into(),
        ack: args.get(4).cloned().unwrap_or_default().into(),
        declared_sha,
        chunk_size,
        wait_for_ack: !simple,
    })
}

/// Consecutive [a,b] sub-ranges of [first,last] with at most `size` pages each
/// (same plan as pdq-wasi, so chunk names and boundaries are identical).
fn plan_chunks(first: usize, last: usize, size: usize) -> Vec<(usize, usize)> {
    (first..=last).step_by(size).map(|a| (a, (a + size - 1).min(last))).collect()
}

fn page_mapping(first: usize, last: usize) -> Vec<serde_json::Value> {
    (first..=last)
        .map(|p| serde_json::json!({"local": p - first + 1, "physical": p}))
        .collect()
}

fn digest(path: &Path) -> std::io::Result<(String, u64)> {
    let mut file = fs::File::open(path)?;
    let mut hash = Sha256::new();
    let mut block = vec![0u8; 64 * 1024];
    let mut total = 0u64;
    loop {
        let n = file.read(&mut block)?;
        if n == 0 {
            break;
        }
        hash.update(&block[..n]);
        total += n as u64;
    }
    Ok((format!("{:x}", hash.finalize()), total))
}

fn existing_outputs(dir: &Path) -> std::io::Result<Vec<String>> {
    let mut found = Vec::new();
    for entry in fs::read_dir(dir)? {
        let name = entry?.file_name().to_string_lossy().into_owned();
        if ["chunk.pdf", "manifest.json", "ready"].contains(&name.as_str()) || name.starts_with("chunk-") {
            found.push(name);
        }
    }
    found.sort();
    Ok(found)
}

type Source = Pdf<BufReader<Counting>>;

struct Outcome {
    io: Rc<IoStats>,
    warnings: Vec<String>,
    open_ms: u64,
    extract_ms: u64,
    write_ms: u64,
    source_pages: usize,
}

fn ms(t: Instant) -> u64 {
    t.elapsed().as_millis() as u64
}

/// Open the original once through `Read + Seek`; refuse encrypted input.
fn open_source(input: &Path) -> Result<(Source, Rc<IoStats>, Vec<String>), String> {
    let io = Rc::new(IoStats::default());
    let file = fs::File::open(input).map_err(|e| format!("open original: {e}"))?;
    let cap = std::env::var("CHUNKER_READ_BUF").ok().and_then(|v| v.parse().ok()).unwrap_or(2 * 1024);
    let reader = BufReader::with_capacity(cap, Counting { file, pos: 0, stats: io.clone() });
    let pdf = Pdf::open(reader).map_err(|e| format!("flpdf open: {e}"))?;
    if pdf.is_encrypted() {
        return Err("encrypted input refused (policy undecided)".into());
    }
    let warnings = pdf.repair_diagnostics().entries().iter().map(|w| format!("repair: {w}")).collect();
    Ok((pdf, io, warnings))
}

fn source_page_count(pdf: &mut Source) -> Result<usize, String> {
    Ok(PageDocumentHelper::new(pdf).get_all_pages().map_err(|e| format!("page tree: {e}"))?.len())
}

/// Per-chunk phase timings (ms).
#[derive(Default, Clone, Copy)]
struct Phases {
    extract: u64,
    prune: u64,
    write: u64,
}

/// Extract physical pages [first, last] (1-based) of an open source into
/// `output`. The extracted document lives only for this call.
/// Batch-only "lean" extraction: the same public-API steps as
/// `flpdf::extract_pages` (fresh `Pdf::empty`, `copy_foreign_object` per page
/// through one per-target copy map, `/Parent`, `/Kids`, `/Count`), but without
/// re-running `push_inherited_attributes_to_pages` on every call: the batch
/// pushes once after opening, and nothing between chunks mutates the source
/// page tree. Only used when the source has no `/PageLabels` (the caller falls
/// back to the library route otherwise). Must stay byte-identical to the
/// library route (checked by the benchmark's per-chunk sha256 comparison).
fn extract_lean(pdf: &mut Source, all_pages: &[ObjectRef], first: usize, last: usize) -> flpdf::Result<Pdf<std::io::Cursor<Vec<u8>>>> {
    let mut target = Pdf::empty()?;
    let pages_root = target.root_handle()?.try_get_key(b"/Pages")?;
    let mut kids = Vec::with_capacity(last - first + 1);
    for &page_ref in &all_pages[first - 1..last] {
        let source_page = pdf.get_object_handle(page_ref);
        kids.push(target.copy_foreign_object(&source_page)?);
    }
    for kid in &kids {
        kid.replace_key(b"/Parent", pages_root.clone())?;
    }
    let count = kids.len() as i64;
    pages_root.replace_key(b"/Kids", ObjectHandle::array(kids))?;
    pages_root.replace_key(b"/Count", ObjectHandle::integer(count))?;
    Ok(target)
}

fn write_range(pdf: &mut Source, first: usize, last: usize, output: &Path) -> Result<Phases, String> {
    write_range_with(pdf, None, first, last, output)
}

fn write_range_with(pdf: &mut Source, lean: Option<&[ObjectRef]>, first: usize, last: usize, output: &Path) -> Result<Phases, String> {
    let t1 = Instant::now();
    let indices: Vec<usize> = (first - 1..last).collect();
    let mut extracted = match lean {
        Some(all_pages) => extract_lean(pdf, all_pages, first, last).map_err(|e| format!("flpdf lean extract: {e}"))?,
        None => extract_pages(pdf, &indices).map_err(|e| format!("flpdf extract_pages: {e}"))?,
    };
    let extract = ms(t1);
    // qpdf's `--pages` prunes unused /Font and /XObject entries by default;
    // flpdf's library-level extract_pages does not. Without this, a document
    // whose pages share one /Resources dict copies every image into each
    // chunk (observed: 1,242 KiB for a 2-page digital chunk).
    let t2 = Instant::now();
    PageDocumentHelper::new(&mut extracted)
        .remove_unreferenced_resources()
        .map_err(|e| format!("flpdf remove_unreferenced_resources: {e}"))?;
    let prune = ms(t2);

    let t3 = Instant::now();
    let out = fs::File::create(output).map_err(|e| format!("create chunk: {e}"))?;
    {
        let mut writer = PdfWriter::new(&mut extracted);
        writer.set_deterministic_id(true);
        writer.set_output_writer(std::io::BufWriter::new(out)).map_err(|e| format!("writer: {e}"))?;
        writer.write().map_err(|e| format!("flpdf write: {e}"))?;
    }
    Ok(Phases { extract, prune, write: ms(t3) })
}

/// Extract physical pages [first, last] (1-based) into `output`.
fn extract(input: &Path, first: usize, last: usize, output: &Path) -> Result<Outcome, String> {
    let t0 = Instant::now();
    let (mut pdf, io, mut warnings) = open_source(input)?;
    let open_ms = ms(t0);
    let t1 = Instant::now();
    let source_pages = source_page_count(&mut pdf)?;
    if last > source_pages {
        return Err(format!("range ends at {last} but the document has {source_pages} pages"));
    }
    let page_tree_ms = ms(t1);
    let ph = write_range(&mut pdf, first, last, output)?;
    warnings.extend(pdf.get_warnings().entries().iter().map(|w| format!("warning: {w}")));
    Ok(Outcome { io, warnings, open_ms, extract_ms: page_tree_ms + ph.extract + ph.prune, write_ms: ph.write, source_pages })
}

fn derivative_pages(path: &Path) -> Result<usize, String> {
    let file = fs::File::open(path).map_err(|e| format!("reopen chunk: {e}"))?;
    let mut pdf = Pdf::open(BufReader::new(file)).map_err(|e| format!("reopen chunk: {e}"))?;
    let n = PageDocumentHelper::new(&mut pdf).get_all_pages().map_err(|e| format!("chunk page tree: {e}"))?.len();
    Ok(n)
}

fn run_chunk(r: &Request, output: &Path) -> Result<serde_json::Value, String> {
    let started = Instant::now();
    let source_bytes = fs::metadata(&r.input).map_err(|e| format!("stat original: {e}"))?.len();
    let o = extract(&r.input, r.first, r.last, output)?;
    let linear_after_extract = linear_memory_bytes();
    let count = derivative_pages(output)?;
    let want = r.last - r.first + 1;
    if count != want {
        return Err(format!("derivative has {count} pages, expected {want}"));
    }
    let (sha, bytes) = digest(output).map_err(|e| format!("hash chunk: {e}"))?;
    Ok(serde_json::json!({
        "schema": "viii.pdf-chunk.v1",
        "tool": "flpdf",
        "tool_version": FLPDF_VERSION,
        "target": std::env::consts::OS,
        "source_bytes": source_bytes,
        "source_page_count": o.source_pages,
        "source_sha256_declared": r.declared_sha,
        "physical_first": r.first,
        "physical_last": r.last,
        "pages": page_mapping(r.first, r.last),
        "chunk_sha256": sha,
        "chunk_bytes": bytes,
        "chunk_page_count": count,
        "diagnostics": o.warnings,
        "resource_pruning": "remove_unreferenced_resources (Font, XObject)",
        "io": {
            "source_bytes_read": o.io.bytes.get(),
            "source_read_calls": o.io.reads.get(),
            "source_seeks": o.io.seeks.get(),
        },
        "guest_linear_memory_after_extract": linear_after_extract,
        "guest_linear_memory_final": linear_memory_bytes(),
        "open_ms": o.open_ms,
        "extract_ms": o.extract_ms,
        "write_ms": o.write_ms,
        "elapsed_ms": started.elapsed().as_millis() as u64,
    }))
}

/// Batch mode: open once, then extract, prune, write and verify each chunk in
/// turn. Only one extracted document is alive at a time. Each chunk is followed
/// by a sidecar JSON so a host can verify and use it before `ready`.
fn run_batch(r: &Request, size: usize) -> Result<serde_json::Value, String> {
    let started = Instant::now();
    let source_bytes = fs::metadata(&r.input).map_err(|e| format!("stat original: {e}"))?.len();
    let t0 = Instant::now();
    let (mut pdf, io, mut warnings) = open_source(&r.input)?;
    let open_ms = ms(t0);
    let linear_after_open = linear_memory_bytes();

    // Index preparation: the page list (flpdf caches it) and the qpdf-style
    // push of inherited attributes, which extract_pages repeats on every call.
    let t1 = Instant::now();
    let source_pages = source_page_count(&mut pdf)?;
    let page_tree_ms = ms(t1);
    let t2 = Instant::now();
    PageDocumentHelper::new(&mut pdf)
        .push_inherited_attributes_to_pages()
        .map_err(|e| format!("push inherited attributes: {e}"))?;
    let first_push_ms = ms(t2);
    let io_after_index = (io.bytes.get(), io.reads.get(), io.seeks.get());
    let linear_after_index = linear_memory_bytes();
    let last = if r.last == 0 { source_pages } else { r.last };
    if last > source_pages || r.first > last {
        return Err(format!("range {}-{last} is outside the document's {source_pages} pages", r.first));
    }
    // Diagnostic only (native runs): time one extra push per chunk to measure
    // the cost of the repeated page-tree walk inside extract_pages.
    let probe_push = std::env::var("FLPDF_PROBE_PUSH").is_ok_and(|v| v == "1");
    // Lean route by default (FLPDF_EXTRACT=library opts out): skip the per-call page-tree walk.
    // Falls back to the library route when the source has /PageLabels.
    let has_labels = pdf.page_labels().has_page_labels().map_err(|e| format!("page labels: {e}"))?;
    let lean_pages = if std::env::var("FLPDF_EXTRACT").map_or(true, |v| v != "library") && !has_labels {
        Some(flpdf::pages::page_refs(&mut pdf).map_err(|e| format!("page refs: {e}"))?)
    } else {
        None
    };
    let extract_route = if lean_pages.is_some() { "lean" } else { "library" };

    let mut chunks = Vec::new();
    let mut totals = Phases::default();
    let (mut verify_total, mut push_probe_total) = (0u64, 0u64);
    for (a, b) in plan_chunks(r.first, last, size) {
        let name = format!("chunk-{a:06}-{b:06}");
        let path = r.out.join(format!("{name}.pdf"));
        let io_before = (io.bytes.get(), io.reads.get(), io.seeks.get());
        let push_probe_ms = if probe_push {
            let t = Instant::now();
            PageDocumentHelper::new(&mut pdf)
                .push_inherited_attributes_to_pages()
                .map_err(|e| format!("push probe: {e}"))?;
            Some(ms(t))
        } else {
            None
        };
        let ph = write_range_with(&mut pdf, lean_pages.as_deref(), a, b, &path)?;
        let t3 = Instant::now();
        let count = derivative_pages(&path)?;
        if count != b - a + 1 {
            return Err(format!("derivative {a}-{b} has {count} pages"));
        }
        let (sha, bytes) = digest(&path).map_err(|e| format!("hash chunk: {e}"))?;
        let verify_ms = ms(t3);
        totals.extract += ph.extract;
        totals.prune += ph.prune;
        totals.write += ph.write;
        verify_total += verify_ms;
        push_probe_total += push_probe_ms.unwrap_or(0);
        // Self-contained sidecar: a host can show progress and bind the chunk
        // to the evidence before the batch manifest exists.
        let mut entry = serde_json::json!({
            "schema": "viii.pdf-chunk-sidecar.v1",
            "tool": "flpdf", "tool_version": FLPDF_VERSION, "extract_route": extract_route,
            "source_page_count": source_pages, "source_sha256_declared": r.declared_sha,
            "chunk_size": size,
            "file": format!("{name}.pdf"),
            "physical_first": a, "physical_last": b, "chunk_page_count": count,
            "pages": page_mapping(a, b), "chunk_sha256": sha, "chunk_bytes": bytes,
        });
        // Sidecar: written after the chunk is complete and verified, under its
        // final name (Bacalhau's /out has no rename).
        fs::write(r.out.join(format!("{name}.json")), entry.to_string()).map_err(|e| format!("write sidecar: {e}"))?;
        entry["metrics"] = serde_json::json!({
            "done_ms": started.elapsed().as_millis() as u64,
            "extract_ms": ph.extract, "prune_ms": ph.prune, "write_ms": ph.write, "verify_ms": verify_ms,
            "push_probe_ms": push_probe_ms,
            "source_bytes_read": io.bytes.get() - io_before.0,
            "source_read_calls": io.reads.get() - io_before.1,
            "source_seeks": io.seeks.get() - io_before.2,
            "guest_linear_memory": linear_memory_bytes(),
        });
        chunks.push(entry);
    }
    warnings.extend(pdf.get_warnings().entries().iter().map(|w| format!("warning: {w}")));
    Ok(serde_json::json!({
        "schema": "viii.pdf-chunk-set.v1",
        "tool": "flpdf",
        "tool_version": FLPDF_VERSION,
        "target": std::env::consts::OS,
        "source_bytes": source_bytes,
        "source_page_count": source_pages,
        "source_sha256_declared": r.declared_sha,
        "physical_first": r.first,
        "physical_last": last,
        "chunk_size": size,
        "chunks": chunks,
        "diagnostics": warnings,
        "resource_pruning": "remove_unreferenced_resources (Font, XObject)",
        "extract_route": extract_route,
        "source_has_page_labels": has_labels,
        "phases_ms": {
            "open": open_ms, "page_tree": page_tree_ms, "first_push_inherited": first_push_ms,
            "extract": totals.extract, "prune": totals.prune, "write": totals.write, "verify": verify_total,
            "push_probe": if probe_push { Some(push_probe_total) } else { None },
        },
        "io": {
            "source_bytes_read": io.bytes.get(),
            "source_read_calls": io.reads.get(),
            "source_seeks": io.seeks.get(),
            "index_bytes_read": io_after_index.0,
            "index_read_calls": io_after_index.1,
        },
        "guest_linear_memory_after_open": linear_after_open,
        "guest_linear_memory_after_index": linear_after_index,
        "guest_linear_memory_final": linear_memory_bytes(),
        "elapsed_ms": started.elapsed().as_millis() as u64,
    }))
}

fn wait_ack(path: &Path, timeout: Duration, poll: Duration) -> Result<(), String> {
    let deadline = Instant::now() + timeout;
    while !path.exists() {
        if Instant::now() >= deadline {
            return Err("host acknowledgement timed out".into());
        }
        thread::sleep(poll);
    }
    Ok(())
}

fn run() -> Result<(), String> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let r = parse_args(&args)?;
    if let Some(output) = &r.output {
        let manifest = run_chunk(&r, output)?;
        // Simple contract: one JSON line on stdout (metrics only, no content).
        let _ = std::io::stdout().write_all(format!("{}\n", manifest).as_bytes());
        return Ok(());
    }
    // mkdir on the existing /out mount root returns ENOSYS in Bacalhau v1.9.5.
    if fs::read_dir(&r.out).is_err() {
        fs::create_dir_all(&r.out).map_err(|e| format!("output directory: {e}"))?;
    }
    let found = existing_outputs(&r.out).map_err(|e| format!("list output directory: {e}"))?;
    if !found.is_empty() {
        return Err(format!("refusing to overwrite existing {found:?}"));
    }
    let manifest = match r.chunk_size {
        Some(size) => run_batch(&r, size)?,
        None => run_chunk(&r, &r.out.join("chunk.pdf"))?,
    };
    // Compact for batches (750 chunks x 40-page maps), pretty for one chunk.
    let text = if r.chunk_size.is_some() { manifest.to_string() } else { serde_json::to_string_pretty(&manifest).unwrap() };
    fs::write(r.out.join("manifest.json"), text).map_err(|e| format!("write manifest: {e}"))?;
    fs::write(r.out.join("ready"), b"ready").map_err(|e| format!("write ready: {e}"))?;
    if !r.wait_for_ack {
        return Ok(());
    }
    // A batch host copies and verifies every chunk before acknowledging.
    let timeout = if r.chunk_size.is_some() { 900 } else { 120 };
    wait_ack(&r.ack, Duration::from_secs(timeout), Duration::from_millis(100))
}

fn main() {
    // Exit explicitly: Bacalhau v1.9.5 reports ExitCode -1 when _start returns.
    let ok = match run() {
        Ok(()) => true,
        Err(e) => {
            // One write: Bacalhau v1.9.5 RunOutput keeps only the first write.
            let _ = std::io::stderr().write_all(format!("chunker: {e}\n").as_bytes());
            false
        }
    };
    std::process::exit(if ok { 0 } else { 1 });
}

#[cfg(test)]
mod tests {
    use super::*;

    fn a(v: &[&str]) -> Vec<String> {
        v.iter().map(|s| s.to_string()).collect()
    }

    #[test]
    fn parses_both_contracts() {
        let simple = parse_args(&a(&["in", "3", "5", "o.pdf"])).unwrap();
        assert_eq!(simple.output, Some(PathBuf::from("o.pdf")));
        let sha = "a".repeat(64);
        let worker = parse_args(&a(&["in", "3", "5", "out", "ack", &sha])).unwrap();
        assert_eq!((worker.output, worker.declared_sha.as_deref()), (None, Some(sha.as_str())));
        for bad in [a(&["in", "0", "5", "o"]), a(&["in", "6", "5", "o"]), a(&["in", "1", "30001", "o"]), a(&["in", "1"])] {
            assert!(parse_args(&bad).is_err(), "{bad:?}");
        }
    }

    #[test]
    fn parses_batch_contracts() {
        let sha = "b".repeat(64);
        let pos = parse_args(&a(&["in", "1", "100", "out", "ack", &sha, "40"])).unwrap();
        assert_eq!((pos.chunk_size, pos.wait_for_ack, pos.output), (Some(40), true, None));
        let flags = parse_args(&a(&["in", "--chunk-size", "40", "--out-dir", "o"])).unwrap();
        assert_eq!((flags.first, flags.last, flags.chunk_size, flags.wait_for_ack), (1, 0, Some(40), false));
        let ranged = parse_args(&a(&["in", "--range", "3-9", "--chunk-size", "2", "--out-dir", "o", "--ack", "k"])).unwrap();
        assert_eq!((ranged.first, ranged.last, ranged.wait_for_ack), (3, 9, true));
        for bad in [
            a(&["in", "--chunk-size", "0", "--out-dir", "o"]),
            a(&["in", "--chunk-size", "40"]),
            a(&["in", "--out-dir", "o"]),
            a(&["in", "--chunk-size", "40", "--out-dir", "o", "--range", "9-3"]),
            a(&["in", "--chunk-size", "40", "--out-dir", "o", "--bogus", "1"]),
            a(&["in", "1", "100", "out", "ack", &sha, "1001"]),
        ] {
            assert!(parse_args(&bad).is_err(), "{bad:?}");
        }
    }

    #[test]
    fn batch_plan_matches_pdq() {
        assert_eq!(plan_chunks(29961, 30000, 16), vec![(29961, 29976), (29977, 29992), (29993, 30000)]);
        assert_eq!(plan_chunks(1, 30000, 40).len(), 750);
        let p = plan_chunks(1, 1001, 40);
        assert_eq!((p.len(), *p.last().unwrap()), (26, (1001, 1001)));
    }

    #[test]
    fn mapping_is_physical() {
        let m = page_mapping(29961, 30000);
        assert_eq!(m[38], serde_json::json!({"local": 39, "physical": 29999}));
    }
}
