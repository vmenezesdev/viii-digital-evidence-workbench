#!/usr/bin/env python3
"""Guarded native / Wasmtime runs for the alternative chunkers (ALTERNATIVES.md).

Each run executes one chunker with the simple contract

    chunker original.pdf first last output.pdf

either natively or under Wasmtime (`--wasmtime`, linear memory capped with
`-W max-memory-size`). It reuses bench.run_one: exact OS peak working set and
private bytes read from the child handle after exit, OS read bytes, a host
memory guard that kills the process tree when available RAM drops below
`--min-available-mib`, and a wall-clock timeout. The chunker prints one JSON
line with its own counters (bytes it read from the original, guest linear
memory, phase timings); it is stored under "guest".

The original's sha256 is checked before and after every run. Output is one
JSON object per run appended to --log (JSONL).
"""
import argparse
import hashlib
import json
from pathlib import Path
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parent))
from bench import run_one  # noqa: E402

try:
    import psutil
except ImportError:  # pragma: no cover
    psutil = None


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def build_command(args, original, first, last, output):
    if args.wasmtime:
        pre = Path(original).resolve().parent
        out = Path(output).resolve().parent
        return [args.wasmtime, "run", "-W", f"max-memory-size={args.max_linear_mib << 20}",
                "--dir", f"{pre}::/in", "--dir", f"{out}::/out", args.chunker,
                f"/in/{Path(original).name}", str(first), str(last), f"/out/{Path(output).name}"]
    return [args.chunker, str(original), str(first), str(last), str(output)]


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--chunker", required=True, help="native executable or .wasm module")
    p.add_argument("--name", required=True)
    p.add_argument("--wasmtime", help="path to wasmtime; runs the chunker as a WASI P1 module")
    p.add_argument("--max-linear-mib", type=int, default=1024)
    p.add_argument("--fixture", required=True, help="name=path")
    p.add_argument("--range", required=True, help="first-last (physical, 1-based)")
    p.add_argument("--repeat", type=int, default=1)
    p.add_argument("--out", required=True, type=Path, help="fresh directory for outputs and logs")
    p.add_argument("--log", required=True, type=Path)
    p.add_argument("--min-available-mib", type=int, default=1536)
    p.add_argument("--timeout", type=int, default=600)
    a = p.parse_args(argv)
    a.chunker = str(Path(a.chunker).resolve(strict=True))
    fixture, original = a.fixture.split("=", 1)
    first, last = (int(x) for x in a.range.split("-"))
    a.out.mkdir(parents=True, exist_ok=False)
    before = sha256(original)
    rc = 0
    for i in range(1, a.repeat + 1):
        tag = f"{a.name}-{fixture}-{first}-{last}-r{i}"
        output = a.out / f"{tag}.pdf"
        avail = psutil.virtual_memory().available if psutil else None
        if avail is not None and avail < (a.min_available_mib << 20):
            sys.exit(f"refusing to start {tag}: host available {avail >> 20} MiB < guard")
        cmd = build_command(a, original, first, last, output)
        r = run_one(a.name, cmd, output, a.timeout, a.out, tag=tag, min_available=a.min_available_mib << 20)
        try:
            r["guest"] = json.loads(Path(r["stdout_log"]).read_text(encoding="utf-8").strip().splitlines()[-1])
            r["guest"].pop("pages", None)
        except (IndexError, ValueError):
            r["guest"] = None
        r["stderr"] = Path(r["stderr_log"]).read_text(encoding="utf-8", errors="replace")[:2000]
        r.update(fixture=fixture, first=first, last=last, repeat=i, runtime="wasmtime" if a.wasmtime else "native",
                 host_available_before_bytes=avail, chunk_sha256=sha256(output) if output.exists() else None,
                 at=time.strftime("%Y-%m-%dT%H:%M:%S"))
        after = sha256(original)
        r["original_unchanged"] = after == before
        with a.log.open("a", encoding="utf-8") as log:
            log.write(json.dumps(r) + "\n")
        g = r["guest"] or {}
        print(json.dumps({k: r.get(k) for k in ("candidate", "runtime", "fixture", "returncode", "elapsed_seconds",
                                                 "killed_by_memory_guard", "original_unchanged", "chunk_sha256")}
                         | {"peak_ws_mib": round(r["exact"].get("peak_working_set_bytes", 0) / 2**20, 1),
                            "peak_priv_mib": round(r["exact"].get("peak_private_bytes", 0) / 2**20, 1),
                            "os_read_mib": round(r["exact"].get("read_bytes", 0) / 2**20, 1),
                            "guest_read_mib": round(g.get("io", {}).get("source_bytes_read", 0) / 2**20, 2),
                            "guest_linear_mib": (g.get("guest_linear_memory_final") or 0) >> 20,
                            "stderr": r["stderr"][:300]}))
        if r["returncode"] != 0 or not r["original_unchanged"]:
            rc = 1
            break
    return rc


if __name__ == "__main__":
    raise SystemExit(main())
