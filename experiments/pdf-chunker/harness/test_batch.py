"""Regression tests for the batch experiment (BATCH-RESULTS.md).

Pure-Python tests always run. The worker tests run when FLPDF_CHUNKER points to
a native build of flpdf-wasi (and PDQ_CHUNKER for pdq-wasi); the text check at
chunk boundaries also needs Poppler's pdftotext on PATH.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

from test_bench import load, runner, tiny_pdf

batch = load("batch_bench", "batch_bench.py")
model = load("selection_model", "selection_model.py")
MiB = model.MiB
FLPDF = os.environ.get("FLPDF_CHUNKER")
PDQ = os.environ.get("PDQ_CHUNKER")


def sidecar(root, a, b, data=None, tamper=False):
    data = data if data is not None else f"%PDF {a}-{b}".encode()
    name = f"chunk-{a:06d}-{b:06d}"
    (root / f"{name}.pdf").write_bytes(data)
    entry = {"file": f"{name}.pdf", "physical_first": a, "physical_last": b, "chunk_page_count": b - a + 1,
             "pages": [{"local": i + 1, "physical": p} for i, p in enumerate(range(a, b + 1))],
             "chunk_sha256": "0" * 64 if tamper else hashlib.sha256(data).hexdigest(), "chunk_bytes": len(data)}
    (root / f"{name}.json").write_text(json.dumps(entry))
    return entry


class RunnerBatchTests(unittest.TestCase):
    def test_progressive_first_chunk_verifies_hash_and_map(self):
        with tempfile.TemporaryDirectory() as d:
            out, dest = Path(d) / "out", Path(d) / "dest"
            out.mkdir()
            self.assertIsNone(runner.progressive_first_chunk(out, dest))
            sidecar(out, 41, 80)
            sidecar(out, 1, 40)
            got = runner.progressive_first_chunk(out, dest)
            self.assertEqual((got["file"], got["verified"], got["sidecars_visible"]), ("chunk-000001-000040.pdf", True, 2))
            self.assertTrue((dest / "chunk-000001-000040.pdf").is_file())

    def test_progressive_first_chunk_rejects_tampered_or_partial(self):
        with tempfile.TemporaryDirectory() as d:
            out = Path(d) / "out"
            out.mkdir()
            sidecar(out, 1, 40, tamper=True)
            self.assertFalse(runner.progressive_first_chunk(out, Path(d) / "dest")["verified"])
            (out / "chunk-000001-000040.json").write_text('{"file": "chunk-0000')  # half-written sidecar
            self.assertIsNone(runner.progressive_first_chunk(out, Path(d) / "dest2"))

    def test_job_spec_env_goes_to_task_env(self):
        # Bacalhau v1.9.5 ignores Engine.Params.EnvironmentVariables; Task.Env reaches the guest.
        spec = runner.job_spec("n", "node", Path("m.wasm"), {}, Path("ctl"), ["a"], "1gb", "1", 60,
                               env={"FLPDF_EXTRACT": "lean"})
        task = spec["Job"]["Tasks"][0]
        self.assertEqual(task["Env"], {"FLPDF_EXTRACT": "lean"})
        self.assertNotIn("EnvironmentVariables", task["Engine"]["Params"])
        self.assertNotIn("Env", runner.job_spec("n", "node", Path("m.wasm"), {}, Path("ctl"), ["a"], "1gb", "1", 60)
                         ["Job"]["Tasks"][0])


class HarnessTests(unittest.TestCase):
    def test_set_digest_depends_on_order_and_hashes(self):
        a = {"physical_first": 1, "physical_last": 40, "chunk_sha256": "a" * 64}
        b = {"physical_first": 41, "physical_last": 80, "chunk_sha256": "b" * 64}
        self.assertNotEqual(batch.set_digest([a, b]), batch.set_digest([b, a]))
        self.assertNotEqual(batch.set_digest([a, b]), batch.set_digest([a, b | {"chunk_sha256": "c" * 64}]))

    def test_last_page_and_variants(self):
        self.assertEqual(batch.last_page(30000, "10", 40), 400)
        self.assertEqual(batch.last_page(1000, "50", 40), 1000)
        self.assertEqual(batch.last_page(1000, "all", 40), 1000)
        self.assertEqual(batch.base("flpdf-lean"), "flpdf")

        class A:
            env = ["flpdf-lean=FLPDF_EXTRACT=lean,CHUNKER_READ_BUF=2048"]
        self.assertEqual(batch.impl_env(A, "flpdf-lean"), {"FLPDF_EXTRACT": "lean", "CHUNKER_READ_BUF": "2048"})
        self.assertEqual(batch.impl_env(A, "flpdf"), {})


class PolicyTests(unittest.TestCase):
    C = {"pdq-wasi": {"mem": [0, 2.0, 0.0], "time": [10.0, 0.0, 0.0], "linear": [0, 1.0, 0.0]},
         "flpdf-wasi": {"mem": [100 * MiB, 0.0, 0.0], "time": [100.0, 0.0, 0.0], "linear": [0, 0.0, 0.0]}}

    def test_pdq_when_safe_and_much_faster(self):
        self.assertEqual(model.choose(100 * MiB, 1000, 4096 * MiB, coeffs=self.C)["choice"], "pdq-wasi")

    def test_flpdf_when_pdq_unsafe(self):
        d = model.choose(1500 * MiB, 30000, 3000 * MiB, coeffs=self.C)
        self.assertEqual((d["choice"], d["pdq-wasi"]["safe"]), ("flpdf-wasi", False))

    def test_reserved_memory_counts(self):
        self.assertEqual(model.choose(100 * MiB, 1000, 4096 * MiB, reserved=3800 * MiB, coeffs=self.C)["choice"],
                         "reject")

    def test_wasm32_ceiling(self):
        d = model.choose(2500 * MiB * 2, 1000, 1 << 40, coeffs=self.C)  # linear 5 GiB > 4 GiB
        self.assertFalse(d["pdq-wasi"]["safe"])

    def test_marginal_speedup_prefers_single_strategy(self):
        c = self.C | {"flpdf-wasi": self.C["flpdf-wasi"] | {"time": [15.0, 0.0, 0.0]}}
        self.assertEqual(model.choose(100 * MiB, 1000, 4096 * MiB, coeffs=c)["choice"], "flpdf-wasi")


@unittest.skipUnless(FLPDF, "FLPDF_CHUNKER (native flpdf-wasi build) not set")
class FlpdfBatchWorkerTests(unittest.TestCase):
    def run_batch(self, src, out, size, extra_env=None, args=()):
        env = os.environ | (extra_env or {})
        return subprocess.run([FLPDF, str(src), "--chunk-size", str(size), "--out-dir", str(out), *args],
                              env=env, capture_output=True, text=True, timeout=120)

    def test_batch_tiles_range_with_sidecars_and_physical_map(self):
        with tempfile.TemporaryDirectory() as d:
            src, out = Path(d) / "in.pdf", Path(d) / "out"
            tiny_pdf(src, 7)
            r = self.run_batch(src, out, 3)
            self.assertEqual(r.returncode, 0, r.stderr)
            ok, reason, _ = runner.verify_chunkset_dir(out, 1, 7)
            self.assertTrue(ok, reason)
            m = json.loads((out / "manifest.json").read_text())
            self.assertEqual([(c["physical_first"], c["physical_last"]) for c in m["chunks"]], [(1, 3), (4, 6), (7, 7)])
            for c in m["chunks"]:
                side = json.loads((out / c["file"].replace(".pdf", ".json")).read_text())
                self.assertEqual(side, {k: v for k, v in c.items() if k != "metrics"})
            self.assertEqual(m["extract_route"], "library")
            self.assertNotIn("PHYSICAL", r.stdout + r.stderr)

    def test_lean_route_is_byte_identical(self):
        with tempfile.TemporaryDirectory() as d:
            src = Path(d) / "in.pdf"
            tiny_pdf(src, 9)
            lib, lean = Path(d) / "lib", Path(d) / "lean"
            self.assertEqual(self.run_batch(src, lib, 4).returncode, 0)
            self.assertEqual(self.run_batch(src, lean, 4, {"FLPDF_EXTRACT": "lean"}).returncode, 0)
            a, b = (json.loads((p / "manifest.json").read_text()) for p in (lib, lean))
            self.assertEqual(b["extract_route"], "lean")
            self.assertEqual([c["chunk_sha256"] for c in a["chunks"]], [c["chunk_sha256"] for c in b["chunks"]])

    def test_refuses_existing_outputs_and_bad_ranges(self):
        with tempfile.TemporaryDirectory() as d:
            src, out = Path(d) / "in.pdf", Path(d) / "out"
            tiny_pdf(src, 3)
            self.assertEqual(self.run_batch(src, out, 2).returncode, 0)
            self.assertNotEqual(self.run_batch(src, out, 2).returncode, 0)  # never overwrite
            self.assertNotEqual(self.run_batch(src, Path(d) / "o2", 2, args=("--range", "2-9")).returncode, 0)

    @unittest.skipUnless(shutil.which("pdftotext"), "Poppler pdftotext required")
    def test_chunk_boundaries_keep_physical_pages(self):
        with tempfile.TemporaryDirectory() as d:
            src, out = Path(d) / "in.pdf", Path(d) / "out"
            tiny_pdf(src, 8)
            self.assertEqual(self.run_batch(src, out, 3).returncode, 0)
            m = json.loads((out / "manifest.json").read_text())
            for c in m["chunks"]:
                for entry in c["pages"]:
                    txt = subprocess.run(["pdftotext", "-f", str(entry["local"]), "-l", str(entry["local"]),
                                          str(out / c["file"]), "-"], capture_output=True, text=True).stdout
                    self.assertIn(f"PHYSICAL {entry['physical']}", txt)

    @unittest.skipUnless(PDQ, "PDQ_CHUNKER (native pdq-wasi build) not set")
    def test_pdq_and_flpdf_sets_have_same_boundaries(self):
        with tempfile.TemporaryDirectory() as d:
            src = Path(d) / "in.pdf"
            tiny_pdf(src, 7)
            self.assertEqual(self.run_batch(src, Path(d) / "f", 3).returncode, 0)
            ack = Path(d) / "ack"
            ack.write_text("accepted")
            sha = hashlib.sha256(src.read_bytes()).hexdigest()
            r = subprocess.run([PDQ, str(src), "1", "7", str(Path(d) / "p"), str(ack), sha, "3"],
                               capture_output=True, text=True, timeout=120)
            self.assertEqual(r.returncode, 0, r.stderr)
            f, p = (json.loads((Path(d) / x / "manifest.json").read_text())["chunks"] for x in ("f", "p"))
            strip = lambda cs: [(c["file"], c["physical_first"], c["physical_last"], c["pages"]) for c in cs]
            self.assertEqual(strip(f), strip(p))


if __name__ == "__main__":
    unittest.main()
