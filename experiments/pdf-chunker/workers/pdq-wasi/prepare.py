#!/usr/bin/env python3
"""Fetch the pinned pdq crate, verify its checksum and apply the WASI patch.

Produces ./vendor-pdq (git-ignored). Refuses to reuse an existing directory.
"""
import hashlib
import io
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import urllib.request

HERE = Path(__file__).resolve().parent
CRATE = "pdq"
VERSION = "0.5.1"
# sha256 of https://static.crates.io/crates/pdq/pdq-0.5.1.crate (crates.io index checksum)
SHA256 = "e26a6b54707d73164c5e20255f2bdb0176785889e7cbe4f7c1083461995d5370"
PATCH = HERE / "wasi-read-instead-of-mmap.patch"


def main():
    target = HERE / "vendor-pdq"
    if target.exists():
        if "--force" not in sys.argv:
            sys.exit(f"{target} exists; pass --force to rebuild it")
        shutil.rmtree(target)
    url = f"https://static.crates.io/crates/{CRATE}/{CRATE}-{VERSION}.crate"
    with urllib.request.urlopen(url, timeout=60) as response:
        data = response.read()
    digest = hashlib.sha256(data).hexdigest()
    if digest != SHA256:
        sys.exit(f"checksum mismatch for {url}: {digest}")
    staging = HERE / "vendor-pdq.tmp"
    shutil.rmtree(staging, ignore_errors=True)
    staging.mkdir()
    prefix = f"{CRATE}-{VERSION}/"
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as archive:
        for member in archive.getmembers():
            if not member.name.startswith(prefix) or not (member.isfile() or member.isdir()):
                sys.exit(f"unexpected archive member: {member.name}")
            relative = member.name[len(prefix):]
            if not relative or ".." in Path(relative).parts:
                continue
            path = staging / relative
            if member.isdir():
                path.mkdir(parents=True, exist_ok=True)
            else:
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(archive.extractfile(member).read())
    # Run git apply outside repository discovery so paths are relative to staging.
    env = dict(os.environ, GIT_CEILING_DIRECTORIES=str(HERE))
    subprocess.run(["git", "apply", "--whitespace=nowarn", str(PATCH)],
                   cwd=staging, env=env, check=True)
    staging.rename(target)
    print(f"prepared {target} from {url} sha256={digest}")


if __name__ == "__main__":
    main()
