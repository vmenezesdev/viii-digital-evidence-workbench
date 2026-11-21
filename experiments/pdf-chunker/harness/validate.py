#!/usr/bin/env python3
"""Compare every page of a derived chunk with its physical page in the original.

Independent of all candidates: uses Poppler (pdftotext, pdftoppm) and qpdf.
For local page n of the chunk, the physical page is first + n - 1.
  * text_equal: pdftotext -layout output identical;
  * raster_equal: pdftoppm grayscale rendering at --dpi byte-identical.
Raster identity at a low DPI is strong evidence of visual identity for these
fixtures, not a proof for arbitrary PDFs (e.g. resources differing below the
sampling resolution). Prints JSON; never prints page text.
"""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def _tool(name):
    path = shutil.which(name)
    if not path:
        raise RuntimeError(f"{name} (Poppler) is required for content validation")
    return path


def page_texts(pdf, first, last):
    out = subprocess.run([_tool("pdftotext"), "-layout", "-enc", "UTF-8", "-f", str(first), "-l", str(last),
                          str(pdf), "-"], capture_output=True, timeout=1800)
    if out.returncode:
        raise RuntimeError(f"pdftotext failed: {out.stderr[-300:]!r}")
    pages = out.stdout.split(b"\f")
    return pages[: last - first + 1]


def page_rasters(pdf, first, last, dpi, workdir, prefix):
    out = subprocess.run([_tool("pdftoppm"), "-gray", "-r", str(dpi), "-f", str(first), "-l", str(last),
                          str(pdf), str(Path(workdir) / prefix)], capture_output=True, timeout=1800)
    if out.returncode:
        raise RuntimeError(f"pdftoppm failed: {out.stderr[-300:]!r}")
    files = sorted(Path(workdir).glob(prefix + "-*.pgm"), key=lambda p: int(p.stem.rsplit("-", 1)[1]))
    return [hashlib.sha256(f.read_bytes()).hexdigest() for f in files]


def compare_range(original, first, last, derived, dpi=30, markers=("JABUTICABA",)):
    n = last - first + 1
    orig_text = page_texts(original, first, last)
    der_text = page_texts(derived, 1, n)
    with tempfile.TemporaryDirectory() as tmp:
        orig_raster = page_rasters(original, first, last, dpi, tmp, "o")
        der_raster = page_rasters(derived, 1, n, dpi, tmp, "d")
    pages = []
    for i in range(n):
        ot = orig_text[i] if i < len(orig_text) else None
        dt_ = der_text[i] if i < len(der_text) else None
        pages.append({
            "local": i + 1, "physical": first + i,
            "text_equal": ot is not None and ot == dt_,
            "text_chars": len(ot.decode("utf-8", "replace").strip()) if ot is not None else None,
            "raster_equal": i < len(orig_raster) and i < len(der_raster) and orig_raster[i] == der_raster[i],
            "raster_sha256": der_raster[i] if i < len(der_raster) else None,
            # Digital text markers only; scanned pages need OCR (separate gate).
            "markers_in_text": sorted(m for m in markers if dt_ is not None and m.encode() in dt_),
        })
    return {
        "method": f"pdftotext -layout; pdftoppm -gray -r {dpi}",
        "pages": pages,
        "text_pages_equal": sum(p["text_equal"] for p in pages),
        "raster_pages_equal": sum(p["raster_equal"] for p in pages),
        "all_equal": all(p["text_equal"] and p["raster_equal"] for p in pages) and len(pages) == n,
    }


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--original", required=True, type=Path)
    p.add_argument("--derived", required=True, type=Path)
    p.add_argument("--first", required=True, type=int)
    p.add_argument("--last", required=True, type=int)
    p.add_argument("--dpi", type=int, default=30)
    p.add_argument("--output", type=Path)
    a = p.parse_args(argv)
    result = compare_range(a.original, a.first, a.last, a.derived, a.dpi)
    text = json.dumps(result, indent=2)
    if a.output:
        a.output.write_text(text, encoding="utf-8")
    print(json.dumps({k: v for k, v in result.items() if k != "pages"}))
    return 0 if result["all_equal"] else 1


if __name__ == "__main__":
    sys.exit(main())
