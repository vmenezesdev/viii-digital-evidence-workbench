"""Adapt Tesseract's locks to the explicitly single threaded WASI worker."""
from pathlib import Path
import sys

root = Path(sys.argv[1])
changed = []
for path in root.joinpath("src").rglob("*"):
    if path.suffix not in (".cpp", ".h"):
        continue
    source = path.read_text()
    patched = source.replace("#include <mutex>", '#include "pdf-wasi-threads.h"')
    patched = patched.replace("#include <thread>", '#include "pdf-wasi-threads.h"')
    for name in ("mutex", "lock_guard", "thread", "this_thread"):
        patched = patched.replace("std::" + name, "viii_serial::" + name)
    if patched != source:
        path.write_text(patched)
        changed.append(str(path))
assert changed, "Tesseract source changed: review the single threaded adaptation"
print("Single threaded WASI adaptation:", *changed, sep="\n")
cmake = root / "CMakeLists.txt"
source = cmake.read_text()
needle = "add_library(libtesseract ${LIBTESSFILES})"
assert source.count(needle) == 1
cmake.write_text(source.replace(needle, 'list(FILTER LIBTESSFILES EXCLUDE REGEX "/viewer/")\n' + needle))
