#!/usr/bin/env python3
"""Adversarial chunking tests on synthetic derivatives of a fixture.

  python adversarial.py make --base s12.pdf --out DIR      # needs qpdf
  python adversarial.py run  --fixtures DIR --results DIR [--wasm pdq=... pdfcpu=...]

Cases (all synthetic, generated from the base fixture):
  shared-resources  every page uses ONE /Resources dict holding every image
  object-streams    objects packed in object streams + xref stream (qpdf)
  incremental       an appended revision replaces page 2's text (marker REVISADO)
  broken-xref       every xref offset shifted by +7 bytes (lying table)
  truncated         file cut before the xref/trailer
  encrypted-owner   AES-256, empty user password (opens without password)
  encrypted-user    AES-256, user password required
Oracle: Poppler renders/extracts the same physical page of the input (it
applies incremental updates and repairs xref like viewers do). A candidate
passes a case if it outputs the exact page count and content-identical pages,
or fails cleanly (non-zero exit and, for WASI, no ready marker). A candidate
that exits 0 with wrong content fails the case.
"""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import zlib

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

RANGE = (2, 3)  # physical pages extracted in every case


def qpdf():
    q = shutil.which("qpdf")
    if not q:
        raise SystemExit("qpdf is required")
    return q


def rewrite_xref(data, objects):
    """Serialize {num: body} objects (sorted) with a fresh classic xref."""
    out = bytearray(b"%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
    offsets = {}
    for num in sorted(objects):
        offsets[num] = len(out)
        out += f"{num} 0 obj\n".encode() + objects[num] + b"\nendobj\n"
    size = max(objects) + 1
    xref = len(out)
    out += f"xref\n0 {size}\n0000000000 65535 f \n".encode()
    for n in range(1, size):
        out += (f"{offsets[n]:010d} 00000 n \n" if n in offsets else "0000000000 65535 f \n").encode()
    return bytes(out), xref, size


def parse_objects(data):
    objs = {}
    for m in re.finditer(rb"(\d+) 0 obj\n(.*?)\nendobj\n", data, re.S):
        objs[int(m.group(1))] = m.group(2)
    return objs


def make(base, out):
    out.mkdir(parents=True, exist_ok=False)
    q = qpdf()
    data = base.read_bytes()
    objs = parse_objects(data)
    root = re.search(rb"/Root (\d+) 0 R", data).group(1)

    # shared-resources: one /Resources object referencing every image XObject.
    images = [n for n, b in objs.items() if b.startswith(b"<< /Type /XObject /Subtype /Image")]
    shared_id = max(objs) + 1
    xobjects = b" ".join(f"/Im{n} {n} 0 R".encode() for n in images)
    shared = dict(objs)
    shared[shared_id] = b"<< /Font << /F1 3 0 R >> /XObject << " + xobjects + b" >> >>"
    for n, body in objs.items():
        if body.startswith(b"<< /Type /Page "):
            im = re.search(rb"/XObject << /Im0 (\d+) 0 R >>", body)
            new = re.sub(rb"/Resources << .*? >> >>$", f"/Resources {shared_id} 0 R >>".encode(), body, flags=re.S)
            shared[n] = new
            if im:  # point the page's content at the shared name of its own image
                cid = int(re.search(rb"/Contents (\d+) 0 R", body).group(1))
                raw = zlib.decompress(re.search(rb"stream\n(.*)\nendstream", objs[cid], re.S).group(1))
                raw = raw.replace(b"/Im0 Do", f"/Im{im.group(1).decode()} Do".encode())
                comp = zlib.compress(raw)
                shared[cid] = f"<< /Filter /FlateDecode /Length {len(comp)} >>\nstream\n".encode() + comp + b"\nendstream"
    body, xref, size = rewrite_xref(data, shared)
    (out / "shared-resources.pdf").write_bytes(body + f"trailer\n<< /Size {size} /Root {root.decode()} 0 R >>\nstartxref\n{xref}\n%%EOF\n".encode())

    subprocess.run([q, "--object-streams=generate", "--compress-streams=y", str(base), str(out / "object-streams.pdf")], check=True)

    # incremental: new content object for page 2 + updated page object + xref section with /Prev.
    pages = [n for n, b in objs.items() if b.startswith(b"<< /Type /Page ")]
    kids = [int(k) for k in re.findall(rb"(\d+) 0 R", re.search(rb"/Kids \[(.*?)\]", objs[2]).group(1))]
    page2 = kids[1]
    new_c = max(objs) + 1
    content = b"BT /F1 18 Tf 44 790 Td (REVISADO pagina fisica 2) Tj ET"
    prev = int(re.findall(rb"startxref\n(\d+)", data)[-1])
    inc = bytearray(data)
    off_c = len(inc)
    inc += f"{new_c} 0 obj\n<< /Length {len(content)} >>\nstream\n".encode() + content + b"\nendstream\nendobj\n"
    off_p = len(inc)
    page_body = re.sub(rb"/Contents \d+ 0 R", f"/Contents {new_c} 0 R".encode(), objs[page2])
    inc += f"{page2} 0 obj\n".encode() + page_body + b"\nendobj\n"
    xref = len(inc)
    inc += f"xref\n{page2} 1\n{off_p:010d} 00000 n \n{new_c} 1\n{off_c:010d} 00000 n \n".encode()
    inc += f"trailer\n<< /Size {new_c + 1} /Root {root.decode()} 0 R /Prev {prev} >>\nstartxref\n{xref}\n%%EOF\n".encode()
    (out / "incremental.pdf").write_bytes(bytes(inc))
    assert pages

    # broken-xref: shift every in-use offset by +7.
    head, sep, tail = data.rpartition(b"xref\n")
    fixed = re.sub(rb"(\d{10}) 00000 n ", lambda m: f"{int(m.group(1)) + 7:010d} 00000 n ".encode(), tail)
    (out / "broken-xref.pdf").write_bytes(head + sep + fixed)

    (out / "truncated.pdf").write_bytes(data[: data.rfind(b"xref\n") - 1])

    subprocess.run([q, "--encrypt", "", "owner-secret", "256", "--", str(base), str(out / "encrypted-owner.pdf")], check=True)
    subprocess.run([q, "--encrypt", "user-secret", "owner-secret", "256", "--", str(base), str(out / "encrypted-user.pdf")], check=True)
    print(json.dumps(sorted(p.name for p in out.glob("*.pdf"))))


def run(fixtures, results, wasm, bacalhau):
    import bench
    import validate
    results.mkdir(parents=True, exist_ok=False)
    q = qpdf()
    bins = {n: shutil.which(n) for n in ("pdfcpu", "pdq", "qpdf")}
    if os.environ.get("FLPDF_CHUNKER"):  # ALTERNATIVES.md
        bins["flpdf"] = os.environ["FLPDF_CHUNKER"]
    rows = []
    first, last = RANGE
    for pdf in sorted(fixtures.glob("*.pdf")):
        case = pdf.stem
        before = bench.sha256(pdf)
        try:  # the oracle itself may fail on encrypted-user / truncated
            oracle_ok = validate.compare_range(pdf, first, last, pdf)["all_equal"]
        except Exception as exc:  # noqa: BLE001 - record oracle failure
            oracle_ok = f"oracle failed: {exc}"[:120]
        for cand, binary in bins.items():
            if not binary:
                continue
            out = results / f"{case}-{cand}.pdf"
            rec = bench.run_one(cand, bench.commands(cand, binary, pdf, first, last, out), out, 120, results,
                                f"{case}-{cand}")
            ok_codes = (0, 3) if cand == "qpdf" else (0,)  # qpdf: 3 = succeeded with warnings
            row = judge(case, cand, "native", rec["returncode"] in ok_codes and out.is_file(), out, pdf, oracle_ok,
                        q, validate, rec["returncode"])
            stderr = Path(rec["stderr_log"]).read_text(encoding="utf-8", errors="replace")
            row["reported_damage_or_warning"] = bool(re.search(r"warn|damag|repair|reconstruct", stderr, re.I))
            rows.append(row)
        for cand, module in wasm.items():
            import bacalhau_wasi as runner
            plan = {"module": str(module), "mounts": {"/original.pdf": str(pdf)}, "ready_protocol": True,
                    "memory": "1gb", "timeout_s": 120, "jobs": [{"id": "chunk", "args": [
                        "/original.pdf", str(first), str(last), "/out", "/control/chunk.ack"]}]}
            s = runner.run(plan, (results / f"{case}-{cand}-wasi").resolve(), bacalhau)
            j = s["jobs"][0]
            produced = bool(j.get("acknowledged"))
            out = results / f"{case}-{cand}-wasi" / "artifacts" / "chunk" / "chunk.pdf"
            row = judge(case, cand, "wasi-bacalhau", produced and j.get("exit_code") == 0, out, pdf, oracle_ok, q,
                        validate, j.get("exit_code"))
            row["wasi_state"] = j.get("state")
            manifest = out.parent / "manifest.json"
            if manifest.is_file():
                row["manifest_diagnostics"] = json.loads(manifest.read_text(encoding="utf-8")).get("diagnostics")
            row["verify_reason"] = j.get("verify_reason")
            rows.append(row)
        assert bench.sha256(pdf) == before, f"{case} input mutated"
    (results / "adversarial.json").write_text(json.dumps(rows, indent=1), encoding="utf-8")
    for r in rows:
        print(json.dumps({k: r.get(k) for k in ("case", "candidate", "mode", "outcome", "pages", "content_equal",
                                                "exit_code", "output_kb", "reported_damage_or_warning")}))


def judge(case, cand, mode, produced, out, pdf, oracle_ok, q, validate, code):
    row = {"case": case, "candidate": cand, "mode": mode, "exit_code": code, "oracle": oracle_ok,
           "pages": None, "content_equal": None, "output_kb": None}
    if not produced:
        row["outcome"] = "failed-cleanly"
        return row
    row["output_kb"] = round(out.stat().st_size / 1024, 1)
    count = subprocess.run([q, "--show-npages", str(out)], capture_output=True, text=True)
    row["pages"] = int(count.stdout) if count.returncode == 0 else None
    try:
        v = validate.compare_range(pdf, RANGE[0], RANGE[1], out)
        row["content_equal"] = v["all_equal"]
    except Exception as exc:  # noqa: BLE001
        row["content_equal"] = f"validation failed: {exc}"[:120]
    expected = RANGE[1] - RANGE[0] + 1
    row["outcome"] = "correct" if row["pages"] == expected and row["content_equal"] is True else "WRONG-OUTPUT"
    return row


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("mode", choices=["make", "run"])
    p.add_argument("--base", type=Path)
    p.add_argument("--out", type=Path)
    p.add_argument("--fixtures", type=Path)
    p.add_argument("--results", type=Path)
    p.add_argument("--wasm", nargs="*", default=[])
    p.add_argument("--bacalhau", type=Path, default=Path(r"C:\Bacalhau\bin\bacalhau.exe"))
    a = p.parse_args()
    if a.mode == "make":
        make(a.base.resolve(strict=True), a.out)
    else:
        wasm = {k: Path(v).resolve(strict=True) for k, v in (w.split("=", 1) for w in a.wasm)}
        run(a.fixtures.resolve(strict=True), a.results, wasm, a.bacalhau.resolve(strict=True))


if __name__ == "__main__":
    main()
