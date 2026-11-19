"""Harness regression tests; no external PDF utilities required."""
import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("chunkbench", HERE / "bench.py")
chunkbench = importlib.util.module_from_spec(spec)
spec.loader.exec_module(chunkbench)


class BenchmarkTests(unittest.TestCase):
    def test_sha256_stream(self):
        with tempfile.TemporaryDirectory() as directory:
            p = Path(directory) / "input.bin"
            p.write_bytes(b"abc")
            self.assertEqual(chunkbench.sha256(p),
                             "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")

    def test_run_one_collects_output_and_logs(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            out = root / "result.pdf"
            command = [sys.executable, "-c",
                       "from pathlib import Path; Path(r'%s').write_bytes(b'%%PDF-1.7'); print('OK')" % str(out)]
            record = chunkbench.run_one("mock", command, out, 15, root)
            self.assertEqual(record["returncode"], 0)
            self.assertFalse(record["timed_out"])
            self.assertEqual(record["output_bytes"], len(b"%PDF-1.7"))
            self.assertTrue((root / "mock.stdout.log").exists())

    def test_timeout_is_reported(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            out = root / "missing.pdf"
            record = chunkbench.run_one("slow",
                                        [sys.executable, "-c", "import time;time.sleep(3)"],
                                        out, 0.1, root)
            self.assertTrue(record["timed_out"])
            self.assertIsNone(record["output_bytes"])


if __name__ == "__main__":
    unittest.main()
