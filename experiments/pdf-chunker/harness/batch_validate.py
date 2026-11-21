#!/usr/bin/env python3
"""Forensic validation of batch chunk sets with external oracles (BATCH-RESULTS.md).

  python batch_validate.py --original ORIG.pdf --set pdq=DIR --set flpdf=DIR \
      [--sample first,last,boundary,target:29999 | --all] --out validation.json

For each chunk set (a directory with manifest.json, viii.pdf-chunk-set.v1):
  * the chunk set tiles the range and every chunk hash verifies (runner check);
  * qpdf --check and --show-npages on EVERY chunk (structure, page count);
  * Poppler text and 30-dpi raster of the selected chunks equal to the
    original's physical pages (validate.compare_range), which also proves the
    local -> physical map, including both sides of chunk boundaries;
  * across implementations: the same physical page must have the same
    raster fingerprint and text in both sets (semantic equivalence; the PDF
    bytes are not expected to match between tools);
  * size outliers: chunk bytes per page vs the set median (shared-resource bloat).
Never prints page text.
"""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import statistics
import subprocess
import sys

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import bacalhau_wasi as runner  # noqa: E402
import validate  # noqa: E402


def qpdf_bin():
    q = shutil.which("qpdf")
    if not q:
        raise SystemExit("qpdf is required (PATH)")
    return q


def select(chunks, spec, all_chunks):
    if all_chunks:
        return list(range(len(chunks)))
    idx = set()
    for item in spec.split(","):
        if item == "first":
            idx.add(0)
        elif item == "last":
            idx.add(len(chunks) - 1)
        elif item == "boundary":  # both sides of the middle boundary
            m = len(chunks) // 2
            idx |= {max(0, m - 1), m}
        elif item.startswith("target:"):
            page = int(item.split(":")[1])
            idx |= {i for i, c in enumerate(chunks) if c["physical_first"] <= page <= c["physical_last"]}
    return sorted(idx)


def check_set(original, name, d, spec, all_chunks):
    d = Path(d)
    m = json.loads((d / "manifest.json").read_text(encoding="utf-8"))
    chunks = m["chunks"]
    ok, reason, _ = runner.verify_chunkset_dir(d, m["physical_first"], m["physical_last"])
    q = qpdf_bin()
    qpdf_bad, npages_bad = [], []
    for c in chunks:
        path = d / c["file"]
        chk = subprocess.run([q, "--check", str(path)], capture_output=True, text=True)
        if chk.returncode != 0:  # 3 = warnings, 2 = errors
            qpdf_bad.append({"file": c["file"], "code": chk.returncode})
        n = subprocess.run([q, "--show-npages", str(path)], capture_output=True, text=True).stdout.strip()
        if n != str(c["chunk_page_count"]):
            npages_bad.append({"file": c["file"], "qpdf_npages": n})
    per_page = [c["chunk_bytes"] / c["chunk_page_count"] for c in chunks]
    med = statistics.median(per_page)
    outliers = [{"file": c["file"], "bytes_per_page": round(v), "x_median": round(v / med, 1)}
                for c, v in zip(chunks, per_page) if v > 4 * med]
    pages = {}
    compared = []
    for i in select(chunks, spec, all_chunks):
        c = chunks[i]
        r = validate.compare_range(original, c["physical_first"], c["physical_last"], d / c["file"])
        compared.append({"file": c["file"], "physical_first": c["physical_first"], "physical_last": c["physical_last"],
                         "text_pages_equal": r["text_pages_equal"], "raster_pages_equal": r["raster_pages_equal"],
                         "pages": c["chunk_page_count"], "all_equal": r["all_equal"],
                         "markers": sorted({x for p in r["pages"] for x in p["markers_in_text"]})})
        for p in r["pages"]:
            pages[p["physical"]] = {"raster": p["raster_sha256"], "text_equal": p["text_equal"]}
    return {"set": name, "dir": str(d), "tool": m.get("tool"), "chunk_count": len(chunks),
            "range": [m["physical_first"], m["physical_last"]], "set_verified": ok, "set_reason": reason,
            "qpdf_check_failures": qpdf_bad, "qpdf_npages_mismatches": npages_bad,
            "size": {"total_bytes": sum(c["chunk_bytes"] for c in chunks), "median_bytes_per_page": round(med),
                     "outliers_over_4x_median": outliers},
            "compared_chunks": compared, "compared_pages": len(pages),
            "all_compared_equal": all(x["all_equal"] for x in compared),
            "diagnostics": m.get("diagnostics")}, pages


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--original", required=True, type=Path)
    p.add_argument("--set", nargs="+", required=True, help="name=chunk-set dir")
    p.add_argument("--sample", default="first,boundary,last")
    p.add_argument("--all", action="store_true")
    p.add_argument("--out", required=True, type=Path)
    a = p.parse_args(argv)
    sha_before = runner.sha256(a.original)
    reports, page_maps = [], {}
    for item in a.set:
        name, _, d = item.partition("=")
        rep, pages = check_set(a.original, name, d, a.sample, a.all)
        reports.append(rep)
        page_maps[name] = pages
    cross = None
    if len(page_maps) == 2:
        (n1, p1), (n2, p2) = page_maps.items()
        common = sorted(set(p1) & set(p2))
        cross = {"sets": [n1, n2], "common_pages": len(common),
                 "raster_equal": sum(p1[k]["raster"] == p2[k]["raster"] for k in common),
                 "both_text_equal_to_original": sum(p1[k]["text_equal"] and p2[k]["text_equal"] for k in common)}
    out = {"original": str(a.original), "original_sha256": sha_before,
           "original_unchanged": runner.sha256(a.original) == sha_before,
           "oracles": {"qpdf": subprocess.run([qpdf_bin(), "--version"], capture_output=True, text=True).stdout.split("\n")[0],
                       "poppler": "pdftotext -layout / pdftoppm -gray -r 30"},
           "sets": reports, "cross_implementation": cross}
    a.out.write_text(json.dumps(out, indent=1), encoding="utf-8")
    brief = [{k: r[k] for k in ("set", "chunk_count", "set_verified", "all_compared_equal", "compared_pages")}
             | {"qpdf_fail": len(r["qpdf_check_failures"]), "npages_bad": len(r["qpdf_npages_mismatches"]),
                "outliers": len(r["size"]["outliers_over_4x_median"])} for r in reports]
    print(json.dumps({"sets": brief, "cross": cross, "original_unchanged": out["original_unchanged"]}))
    ok = out["original_unchanged"] and all(r["set_verified"] and r["all_compared_equal"] and not r["qpdf_npages_mismatches"]
                                           for r in reports)
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
