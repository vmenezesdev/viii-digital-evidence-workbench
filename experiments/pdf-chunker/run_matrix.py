#!/usr/bin/env python3
"""Reproduce the chunker comparison matrix (native and WASI/Bacalhau).

  python run_matrix.py native --fixture s1000=path.pdf ... --out DIR
  python run_matrix.py wasi   --fixture ... --module pdq=pdq.wasm --module pdfcpu=pdfcpu.wasm --out DIR
  python run_matrix.py concurrency --fixture s10000=path.pdf --module pdq=... --jobs 1 2 4 --out DIR

Ranges for a document of N pages (physical, 1-based, clipped to [1,N]):
  p1     = [N-1, N-1]               (the scanned target page of the fixtures)
  p32    = [N/2-15, N/2+16]         (middle)
  late40 = [N-39, N]                (end of the document)
Every run writes its own fresh directory; nothing is reused or overwritten.
A host memory guard (default 600 MB available) aborts runs that would push the
workstation into paging; such runs are recorded as lower bounds, not values.
"""
import argparse
import json
from pathlib import Path
import subprocess
import sys

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import bacalhau_wasi as runner  # noqa: E402


def ranges(n):
    clip = lambda a, b: (max(1, a), min(n, b))
    out = {"p1": clip(n - 1, n - 1), "p32": clip(n // 2 - 15, n // 2 + 16), "late40": clip(n - 39, n)}
    seen, unique = set(), {}
    for k, v in out.items():
        if v not in seen:
            seen.add(v)
            unique[k] = v
    return unique


def pairs(values):
    out = {}
    for v in values:
        k, _, path = v.partition("=")
        if not path:
            raise SystemExit(f"expected name=path, got {v}")
        out[k] = Path(path).resolve(strict=True)
    return out


def page_total(pdf):
    meta = pdf.with_suffix(".json")
    if meta.exists():
        return json.loads(meta.read_text(encoding="utf-8"))["pages"]
    raise SystemExit(f"{meta} (fixture manifest) is required to know the page count")


def native(a):
    rows = []
    for name, pdf in pairs(a.fixture).items():
        for label, (first, last) in ranges(page_total(pdf)).items():
            res = a.out / f"native-{name}-{label}"
            cmd = [sys.executable, str(HERE / "bench.py"), "--input", str(pdf), "--first", str(first), "--last",
                   str(last), "--results", str(res), "--repeat", str(a.repeat), "--min-available-mb",
                   str(a.min_available_mb), "--timeout", str(a.timeout)]
            if label == "late40" and a.validate:
                cmd.append("--validate")
            print("RUN", " ".join(cmd[2:]), flush=True)
            subprocess.run(cmd, check=False)
            rows.append({"fixture": name, "range": label, "first": first, "last": last, "results": str(res)})
    return rows


def wasi(a):
    rows = []
    modules = pairs(a.module)
    for name, pdf in pairs(a.fixture).items():
        sha = runner.sha256(pdf)
        for label, (first, last) in ranges(page_total(pdf)).items():
            for rep in range(1, a.repeat + 1):
                for cand, module in modules.items():  # interleave candidates
                    res = a.out / f"wasi-{cand}-{name}-{label}-r{rep}"
                    plan = {"module": str(module), "mounts": {"/original.pdf": str(pdf)}, "ready_protocol": True,
                            "memory": a.memory, "timeout_s": a.timeout, "min_available_mb": a.min_available_mb,
                            "leak_markers": ["JABUTICABA", "Documento", "prova documental", "ARARA"],
                            "jobs": [{"id": "chunk", "args": ["/original.pdf", str(first), str(last), "/out",
                                                              "/control/chunk.ack", sha]}]}
                    print("RUN", res.name, flush=True)
                    s = runner.run(plan, res.resolve(), a.bacalhau.resolve(strict=True))
                    rows.append({"candidate": cand, "fixture": name, "range": label, "repeat": rep,
                                 "results": str(res), "passed": s["passed"]})
    return rows


def startup(a):
    """Control: the module exits immediately on bad arguments -> compile+instantiate cost."""
    rows = []
    for cand, module in pairs(a.module).items():
        for rep in range(1, a.repeat + 1):
            res = a.out / f"startup-{cand}-r{rep}"
            plan = {"module": str(module), "mounts": {}, "ready_protocol": False, "memory": a.memory,
                    "timeout_s": 120, "jobs": [{"id": "startup", "args": ["--startup-control"]}]}
            s = runner.run(plan, res.resolve(), a.bacalhau.resolve(strict=True))
            j = s["jobs"][0]
            rows.append({"candidate": cand, "repeat": rep, "results": str(res), "exit_code": j.get("exit_code"),
                         "terminal_s": j.get("terminal_s"),
                         "node_peak_wset_minus_baseline_rss": s.get("node_peak_wset_minus_baseline_rss"),
                         "node_peak_private_minus_baseline": s.get("node_peak_private_minus_baseline")})
    return rows


def concurrency(a):
    rows = []
    for name, pdf in pairs(a.fixture).items():
        n, sha = page_total(pdf), runner.sha256(pdf)
        for k in a.jobs:
            for cand, module in pairs(a.module).items():
                jobs = []
                for i in range(k):  # distinct late ranges, 40 pages each
                    last = n - 40 * i
                    jobs.append({"id": f"j{i}", "args": ["/original.pdf", str(last - 39), str(last), "/out",
                                                         f"/control/j{i}.ack", sha]})
                res = a.out / f"conc-{cand}-{name}-k{k}"
                plan = {"module": str(module), "mounts": {"/original.pdf": str(pdf)}, "ready_protocol": True,
                        "memory": a.memory, "timeout_s": a.timeout, "min_available_mb": a.min_available_mb,
                        "leak_markers": ["JABUTICABA", "Documento"], "jobs": jobs}
                print("RUN", res.name, flush=True)
                s = runner.run(plan, res.resolve(), a.bacalhau.resolve(strict=True))
                rows.append({"candidate": cand, "fixture": name, "jobs": k, "results": str(res), "passed": s["passed"]})
    return rows


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("mode", choices=["native", "wasi", "startup", "concurrency"])
    p.add_argument("--fixture", nargs="*", default=[])
    p.add_argument("--module", nargs="*", default=[])
    p.add_argument("--out", required=True, type=Path)
    p.add_argument("--repeat", type=int, default=1)
    p.add_argument("--jobs", nargs="*", type=int, default=[1, 2, 4])
    p.add_argument("--memory", default="3gb")
    p.add_argument("--timeout", type=int, default=1800)
    p.add_argument("--min-available-mb", type=int, default=600)
    p.add_argument("--validate", action="store_true")
    p.add_argument("--bacalhau", type=Path, default=Path(r"C:\Bacalhau\bin\bacalhau.exe"))
    a = p.parse_args()
    a.out.mkdir(parents=True, exist_ok=True)
    rows = {"native": native, "wasi": wasi, "startup": startup, "concurrency": concurrency}[a.mode](a)
    index = a.out / f"index-{a.mode}.jsonl"
    with index.open("a", encoding="utf-8") as f:
        for r in rows:
            f.write(json.dumps(r) + "\n")
    print(json.dumps(rows, indent=1))


if __name__ == "__main__":
    main()
