//! Experimental PDF range chunker (otf-pdf backend). See ../ALTERNATIVES.md.
//!
//! Same two contracts as flpdf-wasi:
//!
//!   chunker original.pdf first last output.pdf
//!   chunker original.pdf first last out-dir ack-file [declared-source-sha256]
//!
//! otf-pdf never opens files itself: it reads through `Source::read_at`. This
//! worker implements `Source` over a plain `File` (seek + read under a mutex,
//! position kept in the guest), so the original is never read to the end. The
//! source counts the bytes the guest pulled from the file system; on wasm32
//! the final linear-memory size is recorded as the guest's peak.
use otf_pdf_doc::{Document, SaveOptions};
use otf_pdf_syntax::{OpenOptions, Sink, Source, SourceError};
use sha2::{Digest, Sha256};
use std::{
    fs,
    io::{BufWriter, Read, Seek, SeekFrom, Write},
    path::{Path, PathBuf},
    sync::{
        atomic::{AtomicU64, Ordering::Relaxed},
        Arc, Mutex,
    },
    thread,
    time::{Duration, Instant},
};

const MAX_PAGES: usize = 30000;
const OTF_PDF_VERSION: &str = "0.2.0";

#[derive(Default)]
struct IoStats {
    bytes: AtomicU64,
    reads: AtomicU64,
    seeks: AtomicU64,
}

struct FileSource {
    file: Mutex<(fs::File, u64)>,
    len: u64,
    stats: Arc<IoStats>,
}

impl FileSource {
    fn open(path: &Path, stats: Arc<IoStats>) -> std::io::Result<Self> {
        let file = fs::File::open(path)?;
        let len = file.metadata()?.len();
        Ok(Self { file: Mutex::new((file, 0)), len, stats })
    }
}

impl Source for FileSource {
    fn len(&self) -> u64 {
        self.len
    }

    fn read_at(&self, offset: u64, buf: &mut [u8]) -> Result<usize, SourceError> {
        let mut guard = self.file.lock().map_err(|_| SourceError::new("source lock poisoned"))?;
        let (file, pos) = &mut *guard;
        if offset >= self.len {
            return Ok(0);
        }
        if *pos != offset {
            file.seek(SeekFrom::Start(offset)).map_err(|e| SourceError::new(e.to_string()))?;
            self.stats.seeks.fetch_add(1, Relaxed);
            *pos = offset;
        }
        let mut done = 0;
        while done < buf.len() {
            match file.read(&mut buf[done..]) {
                Ok(0) => break,
                Ok(n) => done += n,
                Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
                Err(e) => return Err(SourceError::new(e.to_string())),
            }
            self.stats.reads.fetch_add(1, Relaxed);
        }
        *pos += done as u64;
        self.stats.bytes.fetch_add(done as u64, Relaxed);
        Ok(done)
    }
}

struct FileSink(BufWriter<fs::File>);
impl Sink for FileSink {
    fn write_all(&mut self, bytes: &[u8]) -> Result<(), SourceError> {
        self.0.write_all(bytes).map_err(|e| SourceError::new(e.to_string()))
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
}

fn parse_args(args: &[String]) -> Result<Request, String> {
    if !(4..=6).contains(&args.len()) {
        return Err("usage: chunker original.pdf first last output.pdf | chunker original.pdf first last out-dir ack-file [source-sha256]".into());
    }
    let first: usize = args[1].parse().map_err(|_| "invalid first page")?;
    let last: usize = args[2].parse().map_err(|_| "invalid last page")?;
    if first == 0 || last < first || last > MAX_PAGES {
        return Err("invalid physical page range".into());
    }
    let declared_sha = match args.get(5) {
        Some(s) if s.len() == 64 && s.bytes().all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b)) => Some(s.clone()),
        Some(_) => return Err("declared source sha256 must be 64 lowercase hex digits".into()),
        None => None,
    };
    let simple = args.len() == 4;
    Ok(Request {
        input: args[0].clone().into(),
        first,
        last,
        output: simple.then(|| args[3].clone().into()),
        out: args[3].clone().into(),
        ack: args.get(4).cloned().unwrap_or_default().into(),
        declared_sha,
    })
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
        if ["chunk.pdf", "manifest.json", "ready"].contains(&name.as_str()) {
            found.push(name);
        }
    }
    found.sort();
    Ok(found)
}

struct Outcome {
    io: Arc<IoStats>,
    warnings: Vec<String>,
    open_ms: u64,
    extract_ms: u64,
    write_ms: u64,
    source_pages: usize,
}

/// Extract physical pages [first, last] (1-based) into `output`.
fn extract(input: &Path, first: usize, last: usize, output: &Path) -> Result<Outcome, String> {
    let t0 = Instant::now();
    let io = Arc::new(IoStats::default());
    let source = FileSource::open(input, io.clone()).map_err(|e| format!("open original: {e}"))?;
    let doc = Document::open(source, OpenOptions::default()).map_err(|e| format!("otf-pdf open: {e}"))?;
    if doc.is_encrypted() {
        return Err("encrypted input refused (policy undecided)".into());
    }
    let open_ms = t0.elapsed().as_millis() as u64;

    let t1 = Instant::now();
    let source_pages = doc.page_count().map_err(|e| format!("page tree: {e}"))?;
    if last > source_pages {
        return Err(format!("range ends at {last} but the document has {source_pages} pages"));
    }
    let indices: Vec<usize> = (first - 1..last).collect();
    let extracted = doc.extract_pages(&indices).map_err(|e| format!("otf-pdf extract_pages: {e}"))?;
    let extract_ms = t1.elapsed().as_millis() as u64;

    let t2 = Instant::now();
    let out = fs::File::create(output).map_err(|e| format!("create chunk: {e}"))?;
    let mut sink = FileSink(BufWriter::new(out));
    extracted.save_to(&mut sink, &SaveOptions::default()).map_err(|e| format!("otf-pdf save: {e}"))?;
    sink.0.flush().map_err(|e| format!("flush chunk: {e}"))?;
    let write_ms = t2.elapsed().as_millis() as u64;
    let warnings = doc.diagnostics().iter().map(|d| format!("repair: {d}")).collect();
    Ok(Outcome { io, warnings, open_ms, extract_ms, write_ms, source_pages })
}

fn derivative_pages(path: &Path) -> Result<usize, String> {
    let source = FileSource::open(path, Arc::new(IoStats::default())).map_err(|e| format!("reopen chunk: {e}"))?;
    let doc = Document::open(source, OpenOptions::default()).map_err(|e| format!("reopen chunk: {e}"))?;
    doc.page_count().map_err(|e| format!("chunk page tree: {e}"))
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
        "tool": "otf-pdf",
        "tool_version": OTF_PDF_VERSION,
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
        "resource_pruning": "none (library extract_pages)",
        "io": {
            "source_bytes_read": o.io.bytes.load(Relaxed),
            "source_read_calls": o.io.reads.load(Relaxed),
            "source_seeks": o.io.seeks.load(Relaxed),
        },
        "guest_linear_memory_after_extract": linear_after_extract,
        "guest_linear_memory_final": linear_memory_bytes(),
        "open_ms": o.open_ms,
        "extract_ms": o.extract_ms,
        "write_ms": o.write_ms,
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
    let manifest = run_chunk(&r, &r.out.join("chunk.pdf"))?;
    fs::write(r.out.join("manifest.json"), serde_json::to_string_pretty(&manifest).unwrap())
        .map_err(|e| format!("write manifest: {e}"))?;
    fs::write(r.out.join("ready"), b"ready").map_err(|e| format!("write ready: {e}"))?;
    wait_ack(&r.ack, Duration::from_secs(120), Duration::from_millis(100))
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
    fn mapping_is_physical() {
        let m = page_mapping(29961, 30000);
        assert_eq!(m[38], serde_json::json!({"local": 39, "physical": 29999}));
    }
}
