//! Experimental PDF range chunker (pdq backend) with the same contract as the
//! Go/pdfcpu worker:
//!
//!   chunker original.pdf first last out-dir ack-file [declared-source-sha256]
//!
//! Writes out-dir/chunk.pdf, manifest.json, then ready (content "ready"), and
//! waits for the host acknowledgement. Never prints PDF content.
//!
//! Bacalhau v1.9.5's WASI /out mount on Windows creates a file on any
//! stat/open of a missing path, ignores O_EXCL and implements neither rename
//! nor remove, so the worker never stats inside out-dir (it lists it), writes
//! final names directly and writes `ready` last. See RESULTS.md.
use serde_json::json;
use sha2::{Digest, Sha256};
use std::{
    fs,
    io::{Read, Write},
    path::{Path, PathBuf},
    thread,
    time::{Duration, Instant},
};

const MAX_PAGES: usize = 30000;
const PDQ_VERSION: &str = "0.5.1+viii-wasi-read-patch";

#[derive(Debug, PartialEq)]
struct Request {
    input: PathBuf,
    first: usize,
    last: usize,
    out: PathBuf,
    ack: PathBuf,
    declared_sha: Option<String>,
    /// Batch mode: split [first,last] into consecutive chunks of this many pages
    /// in ONE pass over the original (one load of the input).
    chunk_size: Option<usize>,
}

fn parse_args(args: &[String]) -> Result<Request, String> {
    if !(5..=7).contains(&args.len()) {
        return Err("usage: chunker original.pdf first last out-dir ack-file [source-sha256 [chunk-size]]".into());
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
    let chunk_size = match args.get(6) {
        Some(v) => match v.parse::<usize>() {
            Ok(n) if n >= 1 && n <= MAX_PAGES => Some(n),
            _ => return Err("invalid chunk size".into()),
        },
        None => None,
    };
    if chunk_size.is_some() && declared_sha.is_none() {
        return Err("batch mode requires the declared source sha256".into());
    }
    Ok(Request {
        input: args[0].clone().into(),
        first,
        last,
        out: args[3].clone().into(),
        ack: args[4].clone().into(),
        declared_sha,
        chunk_size,
    })
}

/// Consecutive [a,b] sub-ranges of [first,last] with at most `size` pages each.
fn plan_chunks(first: usize, last: usize, size: usize) -> Vec<(usize, usize)> {
    (first..=last).step_by(size).map(|a| (a, (a + size - 1).min(last))).collect()
}

fn page_mapping(first: usize, last: usize) -> Vec<serde_json::Value> {
    (first..=last)
        .map(|p| json!({"local": p - first + 1, "physical": p}))
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
        if ["chunk.pdf", "manifest.json", "ready"].contains(&name.as_str())
            || (name.starts_with("chunk-") && name.ends_with(".pdf"))
        {
            found.push(name);
        }
    }
    found.sort();
    Ok(found)
}

struct RemoveUnlessCommitted(PathBuf, bool);
impl Drop for RemoveUnlessCommitted {
    fn drop(&mut self) {
        if !self.1 {
            let _ = fs::remove_file(&self.0);
        }
    }
}

fn chunk(r: &Request) -> Result<serde_json::Value, String> {
    let started = Instant::now();
    let source_bytes = fs::metadata(&r.input).map_err(|e| format!("stat original: {e}"))?.len();
    // mkdir on the existing /out mount root returns ENOSYS in Bacalhau v1.9.5,
    // so only create the directory when it cannot already be listed.
    if fs::read_dir(&r.out).is_err() {
        fs::create_dir_all(&r.out).map_err(|e| format!("output directory: {e}"))?;
    }
    let found = existing_outputs(&r.out).map_err(|e| format!("list output directory: {e}"))?;
    if !found.is_empty() {
        return Err(format!("refusing to overwrite existing {found:?}"));
    }
    let output = r.out.join("chunk.pdf");
    // Best effort: remove is not implemented on Bacalhau's /out.
    let mut guard = RemoveUnlessCommitted(output.clone(), false);
    let range = pdq::PageRangeGroup::parse(format!("{}-{}", r.first, r.last)).map_err(|e| format!("range: {e}"))?;
    pdq::split(&r.input, &[pdq::SplitOutput { range, path: output.clone() }])
        .map_err(|e| format!("pdq split: {e}"))?;
    let count = pdq::page_count(&output).map_err(|e| format!("count derivative pages: {e}"))?;
    let want = r.last - r.first + 1;
    if count != want {
        return Err(format!("derivative has {count} pages, expected {want}"));
    }
    guard.1 = true;
    let (sha, bytes) = digest(&output).map_err(|e| format!("hash chunk: {e}"))?;
    Ok(json!({
        "schema": "viii.pdf-chunk.v1",
        "tool": "pdq",
        "tool_version": PDQ_VERSION,
        "target": std::env::consts::OS,
        "source_bytes": source_bytes,
        "source_sha256_declared": r.declared_sha,
        "physical_first": r.first,
        "physical_last": r.last,
        "pages": page_mapping(r.first, r.last),
        "chunk_sha256": sha,
        "chunk_bytes": bytes,
        "chunk_page_count": count,
        "elapsed_ms": started.elapsed().as_millis() as u64,
    }))
}

fn chunk_set(r: &Request, size: usize) -> Result<serde_json::Value, String> {
    let started = Instant::now();
    let source_bytes = fs::metadata(&r.input).map_err(|e| format!("stat original: {e}"))?.len();
    if fs::read_dir(&r.out).is_err() {
        fs::create_dir_all(&r.out).map_err(|e| format!("output directory: {e}"))?;
    }
    let found = existing_outputs(&r.out).map_err(|e| format!("list output directory: {e}"))?;
    if !found.is_empty() {
        return Err(format!("refusing to overwrite existing {found:?}"));
    }
    let plan = plan_chunks(r.first, r.last, size);
    let mut outputs = Vec::with_capacity(plan.len());
    for (a, b) in &plan {
        let range = pdq::PageRangeGroup::parse(format!("{a}-{b}")).map_err(|e| format!("range: {e}"))?;
        outputs.push(pdq::SplitOutput { range, path: r.out.join(format!("chunk-{a:06}-{b:06}.pdf")) });
    }
    pdq::split(&r.input, &outputs).map_err(|e| format!("pdq split: {e}"))?;
    let split_ms = started.elapsed().as_millis() as u64;
    let mut chunks = Vec::with_capacity(plan.len());
    for ((a, b), out) in plan.iter().zip(&outputs) {
        let count = pdq::page_count(&out.path).map_err(|e| format!("count derivative pages: {e}"))?;
        if count != b - a + 1 {
            return Err(format!("derivative {a}-{b} has {count} pages"));
        }
        let (sha, bytes) = digest(&out.path).map_err(|e| format!("hash chunk: {e}"))?;
        chunks.push(json!({
            "file": out.path.file_name().unwrap().to_string_lossy(),
            "physical_first": a, "physical_last": b, "chunk_page_count": count,
            "pages": page_mapping(*a, *b), "chunk_sha256": sha, "chunk_bytes": bytes,
        }));
    }
    Ok(json!({
        "schema": "viii.pdf-chunk-set.v1",
        "tool": "pdq",
        "tool_version": PDQ_VERSION,
        "target": std::env::consts::OS,
        "source_bytes": source_bytes,
        "source_sha256_declared": r.declared_sha,
        "physical_first": r.first,
        "physical_last": r.last,
        "chunk_size": size,
        "chunks": chunks,
        "split_ms": split_ms,
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
    let manifest = match r.chunk_size {
        Some(size) => chunk_set(&r, size)?,
        None => chunk(&r)?,
    };
    fs::write(r.out.join("manifest.json"), serde_json::to_string_pretty(&manifest).unwrap())
        .map_err(|e| format!("write manifest: {e}"))?;
    fs::write(r.out.join("ready"), b"ready").map_err(|e| format!("write ready: {e}"))?;
    wait_ack(&r.ack, Duration::from_secs(120), Duration::from_millis(100))
}

fn main() {
    // Exit explicitly: Bacalhau v1.9.5 reports ExitCode -1 when _start returns
    // without proc_exit (observed; workers/pdf does the same).
    if run_and_report() {
        std::process::exit(0);
    }
    std::process::exit(1);
}

fn run_and_report() -> bool {
    if let Err(e) = run() {
        // One write: Bacalhau v1.9.5 RunOutput was observed to drop later writes.
        let _ = std::io::stderr().write_all(format!("chunker: {e}\n").as_bytes());
        return false;
    }
    true
}

#[cfg(test)]
mod tests {
    use super::*;

    fn a(v: &[&str]) -> Vec<String> {
        v.iter().map(|s| s.to_string()).collect()
    }

    #[test]
    fn parses_and_rejects_ranges() {
        let sha = "a".repeat(64);
        let ok = parse_args(&a(&["in", "3", "5", "out", "ack", &sha])).unwrap();
        assert_eq!((ok.first, ok.last, ok.declared_sha.as_deref()), (3, 5, Some(sha.as_str())));
        for bad in [
            a(&["in", "0", "5", "out", "ack"]),
            a(&["in", "6", "5", "out", "ack"]),
            a(&["in", "1", "30001", "out", "ack"]),
            a(&["in", "1", "5", "out", "ack", &"A".repeat(64)]),
            a(&["in", "1", "5", "out"]),
        ] {
            assert!(parse_args(&bad).is_err(), "{bad:?}");
        }
    }

    #[test]
    fn batch_plan_covers_range_without_gaps() {
        assert_eq!(plan_chunks(29961, 30000, 16), vec![(29961, 29976), (29977, 29992), (29993, 30000)]);
        assert_eq!(plan_chunks(1, 3, 40), vec![(1, 3)]);
        let sha = "a".repeat(64);
        assert!(parse_args(&a(&["in", "1", "9", "out", "ack", &sha, "4"])).unwrap().chunk_size == Some(4));
        assert!(parse_args(&a(&["in", "1", "9", "out", "ack", &sha, "0"])).is_err());
    }

    #[test]
    fn mapping_is_physical() {
        let m = page_mapping(29961, 30000);
        assert_eq!(m.len(), 40);
        assert_eq!(m[38], json!({"local": 39, "physical": 29999}));
    }

    #[test]
    fn existing_outputs_are_found_by_listing() {
        let dir = std::env::temp_dir().join(format!("pdq-out-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();
        assert!(existing_outputs(&dir).unwrap().is_empty());
        fs::write(dir.join("ready"), b"").unwrap();
        fs::write(dir.join("other.txt"), b"").unwrap();
        assert_eq!(existing_outputs(&dir).unwrap(), vec!["ready".to_string()]);
        fs::remove_dir_all(dir).unwrap();
    }

    #[test]
    fn ack_timeout_and_success() {
        let dir = std::env::temp_dir().join(format!("pdq-ack-{}", std::process::id()));
        fs::create_dir_all(&dir).unwrap();
        let ack = dir.join("accepted");
        let _ = fs::remove_file(&ack);
        assert!(wait_ack(&ack, Duration::from_millis(30), Duration::from_millis(5)).is_err());
        fs::write(&ack, b"ok").unwrap();
        assert!(wait_ack(&ack, Duration::from_millis(30), Duration::from_millis(5)).is_ok());
        fs::remove_dir_all(dir).unwrap();
    }
}
