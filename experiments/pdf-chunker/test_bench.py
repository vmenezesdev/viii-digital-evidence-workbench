"""Harness regression tests; external PDF tools are optional (tests skip)."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import sys
import tempfile
import time
import unittest

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, HERE / file)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


chunkbench = load("chunkbench", "bench.py")
runner = load("bacalhau_wasi", "bacalhau_wasi.py")
validate = load("validate", "validate.py")
e2e = load("e2e_ocr", "e2e_ocr.py")


def tiny_pdf(path, n):
    """Minimal n-page PDF whose page i shows 'PHYSICAL i'."""
    parts, offsets = [b"%PDF-1.7\n"], {}
    size = lambda: sum(len(x) for x in parts)

    def emit(i, body):
        offsets[i] = size()
        parts.append(f"{i} 0 obj\n".encode() + body + b"\nendobj\n")

    kids, nxt = [], 4
    for page in range(1, n + 1):
        pid, cid = nxt, nxt + 1
        nxt += 2
        kids.append(f"{pid} 0 R")
        s = f"BT /F1 24 Tf 20 150 Td (PHYSICAL {page}) Tj ET".encode()
        emit(cid, b"<< /Length %d >>\nstream\n" % len(s) + s + b"\nendstream")
        emit(pid, f"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Contents {cid} 0 R "
                  f"/Resources << /Font << /F1 3 0 R >> >> >>".encode())
    emit(1, b"<< /Type /Catalog /Pages 2 0 R >>")
    emit(2, f"<< /Type /Pages /Count {n} /Kids [{' '.join(kids)}] >>".encode())
    emit(3, b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
    xref = size()
    parts.append(f"xref\n0 {nxt}\n0000000000 65535 f \n".encode())
    parts += [f"{offsets[i]:010d} 00000 n \n".encode() for i in range(1, nxt)]
    parts.append(f"trailer\n<< /Size {nxt} /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n".encode())
    Path(path).write_bytes(b"".join(parts))


class BenchmarkTests(unittest.TestCase):
    def test_sha256_stream(self):
        with tempfile.TemporaryDirectory() as directory:
            p = Path(directory) / "input.bin"
            p.write_bytes(b"abc")
            self.assertEqual(chunkbench.sha256(p),
                             "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")

    def test_cli_syntax_matches_verified_tools(self):
        self.assertEqual(chunkbench.commands("pdq", "pdq", "in.pdf", 3, 5, "o.pdf"),
                         ["pdq", "split", "in.pdf", "--out", "3-5", "o.pdf"])
        self.assertEqual(chunkbench.commands("qpdf", "qpdf", "in.pdf", 3, 5, "o.pdf"),
                         ["qpdf", "--deterministic-id", "--empty", "--pages", "in.pdf", "3-5", "--", "o.pdf"])
        self.assertEqual(chunkbench.commands("pdfcpu", "pdfcpu", "in.pdf", 3, 5, "o.pdf"),
                         ["pdfcpu", "trim", "--pages", "3-5", "in.pdf", "o.pdf"])

    def test_run_one_collects_output_logs_and_exact_metrics(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            out = root / "result.pdf"
            # Allocate ~64 MiB so the exact peak is clearly above interpreter baseline.
            code = ("from pathlib import Path; b=bytearray(64*1024*1024); b[::4096]=b'x'*len(b[::4096]); "
                    "Path(r'%s').write_bytes(b'%%PDF-1.7'); print('OK')" % str(out))
            record = chunkbench.run_one("mock", [sys.executable, "-c", code], out, 30, root)
            self.assertEqual(record["returncode"], 0)
            self.assertFalse(record["timed_out"])
            self.assertEqual(record["output_bytes"], len(b"%PDF-1.7"))
            self.assertTrue((root / "mock.stdout.log").exists())
            peak = record["exact"].get("peak_working_set_bytes") or record["exact"].get("peak_rss_bytes")
            self.assertGreater(peak, 64 * 1024 * 1024)

    def test_timeout_kills_process_tree(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            marker = root / "child.pid"
            code = ("import subprocess,sys,time;"
                    "c=subprocess.Popen([sys.executable,'-c','import time;time.sleep(60)']);"
                    f"open(r'{marker}','w').write(str(c.pid));time.sleep(60)")
            started = time.perf_counter()
            record = chunkbench.run_one("slow", [sys.executable, "-c", code], root / "missing.pdf", 1.5, root)
            self.assertTrue(record["timed_out"])
            self.assertLess(time.perf_counter() - started, 30)
            self.assertIsNone(record["output_bytes"])
            if chunkbench.psutil and marker.exists():
                time.sleep(0.5)
                self.assertFalse(chunkbench.psutil.pid_exists(int(marker.read_text())) and
                                 chunkbench.psutil.Process(int(marker.read_text())).status() != "zombie",
                                 "child survived the timeout")


class RunnerTests(unittest.TestCase):
    def test_pin_requires_version_and_revision(self):
        good = ("\tmod\tgithub.com/bacalhau-project/bacalhau\tv1.9.5\t\n"
                "\tbuild\tvcs.revision=3a6a73aa6529755ac2c9d96a771f0566ff3ee5da\n")
        self.assertTrue(runner.verify_pin(good))
        self.assertFalse(runner.verify_pin(good.replace("v1.9.5", "v1.9.6")))
        self.assertFalse(runner.verify_pin(good.replace("3a6a73", "000000")))

    def test_node_config_disables_docker_and_mounts_read_only(self):
        cfg = runner.node_config(1, 2, "t", [Path("C:/a/b.pdf")])
        self.assertEqual(cfg["Engines"]["Disabled"], ["docker"])
        self.assertTrue(all(p.endswith(":ro") for p in cfg["Compute"]["AllowListedLocalPaths"]))
        self.assertEqual(cfg["API"]["Host"], "127.0.0.1")

    def test_job_spec_has_no_network_and_read_only_inputs(self):
        spec = runner.job_spec("n", "node", Path("m.wasm"), {"/original.pdf": Path("o.pdf")}, Path("ctl"),
                               ["/original.pdf", "1", "2", "/out", "/control/a.ack"], "1gb", "1", 60)
        task = spec["Job"]["Tasks"][0]
        self.assertEqual(task["Network"]["Type"], "none")
        self.assertTrue(all(not i["Source"]["Params"]["ReadWrite"] for i in task["InputSources"]))
        self.assertEqual(task["Engine"]["Type"], "wasm")

    def test_leak_scan(self):
        self.assertEqual(runner.find_leaks("x JABUTICABA y", ["JABUTICABA", "ARARA"]), ["JABUTICABA"])
        self.assertEqual(runner.find_leaks(None, ["JABUTICABA"]), [])

    def _chunk_dir(self, root, ready=b"ready", first=3, last=4, tamper=False):
        root.mkdir()
        (root / "chunk.pdf").write_bytes(b"%PDF-1.7 fake")
        digest = hashlib.sha256(b"%PDF-1.7 fake").hexdigest()
        manifest = {"chunk_sha256": "0" * 64 if tamper else digest, "physical_first": first, "physical_last": last,
                    "chunk_page_count": last - first + 1,
                    "pages": [{"local": i + 1, "physical": p} for i, p in enumerate(range(first, last + 1))]}
        (root / "manifest.json").write_text(json.dumps(manifest))
        if ready is not None:
            (root / "ready").write_bytes(ready)
        return root

    def test_verify_chunk_dir(self):
        with tempfile.TemporaryDirectory() as d:
            ok, reason, _ = runner.verify_chunk_dir(self._chunk_dir(Path(d) / "a"), 3, 4)
            self.assertTrue(ok, reason)
            self.assertFalse(runner.verify_chunk_dir(self._chunk_dir(Path(d) / "b", tamper=True), 3, 4)[0])
            self.assertFalse(runner.verify_chunk_dir(self._chunk_dir(Path(d) / "c"), 2, 4)[0])

    def test_chunkset_must_tile_range(self):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            chunks = []
            for a, b in ((1, 2), (3, 4)):
                data = f"%PDF {a}".encode()
                (root / f"chunk-{a}.pdf").write_bytes(data)
                chunks.append({"file": f"chunk-{a}.pdf", "physical_first": a, "physical_last": b,
                               "chunk_page_count": 2, "chunk_sha256": hashlib.sha256(data).hexdigest(),
                               "chunk_bytes": len(data),
                               "pages": [{"local": 1, "physical": a}, {"local": 2, "physical": b}]})
            (root / "ready").write_bytes(b"ready")
            (root / "manifest.json").write_text(json.dumps({"physical_first": 1, "physical_last": 4, "chunks": chunks}))
            self.assertTrue(runner.verify_chunkset_dir(root, 1, 4)[0])
            self.assertFalse(runner.verify_chunkset_dir(root, 1, 5)[0])
            (root / "manifest.json").write_text(json.dumps({"physical_first": 1, "physical_last": 4,
                                                             "chunks": chunks[::-1]}))
            self.assertFalse(runner.verify_chunkset_dir(root, 1, 4)[0])

    def test_empty_ready_marker_is_rejected(self):
        # Bacalhau's /out creates an empty file when a guest stats a missing path.
        with tempfile.TemporaryDirectory() as d:
            self.assertFalse(runner.verify_chunk_dir(self._chunk_dir(Path(d) / "a", ready=b""), 3, 4)[0])
            self.assertFalse(runner.ready_marker(self._chunk_dir(Path(d) / "b", ready=None)))


class ProvenanceTests(unittest.TestCase):
    MANIFEST = {"pages": [{"local": 1, "physical": 29998}, {"local": 2, "physical": 29999},
                          {"local": 3, "physical": 30000}]}

    def test_citation_points_to_physical_page_of_original(self):
        pages = {1: {"page": 1, "text": "a", "source": "digital"},
                 2: {"page": 2, "text": "x JABUTICABA y", "source": "ocr"},
                 3: {"page": 3, "text": "", "source": "ocr"}}
        mapped = e2e.map_results(self.MANIFEST, pages)
        self.assertEqual(e2e.search(mapped, "jabuticaba"),
                         [{"physical_page": 29999, "chunk_local_page": 2, "source": "ocr"}])

    def test_rejects_results_outside_chunk_or_mislabelled(self):
        with self.assertRaises(ValueError):
            e2e.map_results(self.MANIFEST, {4: {"page": 4}})
        with self.assertRaises(ValueError):
            e2e.map_results(self.MANIFEST, {2: {"page": 3}})
        with self.assertRaises(ValueError):
            e2e.map_results({"pages": [{"local": 2, "physical": 5}]}, {})

    def test_directory_identity_detects_changes(self):
        with tempfile.TemporaryDirectory() as d:
            (Path(d) / "m").mkdir()
            (Path(d) / "m" / "por.traineddata").write_bytes(b"x")
            before = runner.identity(Path(d) / "m")
            self.assertTrue(before.startswith("tree:"))
            (Path(d) / "m" / "por.traineddata").write_bytes(b"y")
            self.assertNotEqual(before, runner.identity(Path(d) / "m"))


@unittest.skipUnless(shutil.which("pdftotext") and shutil.which("pdftoppm"), "Poppler not installed")
class ValidateTests(unittest.TestCase):
    def test_detects_equal_and_shifted_ranges(self):
        with tempfile.TemporaryDirectory() as d:
            original, wrong = Path(d) / "o.pdf", Path(d) / "w.pdf"
            tiny_pdf(original, 5)
            tiny_pdf(wrong, 2)  # pages say PHYSICAL 1..2, not 4..5
            same = validate.compare_range(original, 1, 2, wrong)
            self.assertTrue(same["all_equal"])
            shifted = validate.compare_range(original, 4, 5, wrong)
            self.assertFalse(shifted["all_equal"])
            self.assertEqual([p["physical"] for p in shifted["pages"]], [4, 5])


if __name__ == "__main__":
    unittest.main()
