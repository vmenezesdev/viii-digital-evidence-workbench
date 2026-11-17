// Run after building bin/worker-native[.exe] and bin/worker.wasm.
// Node's preview1 WASI runtime verifies the worker contract, not Bacalhau integration.
const { WASI } = require('node:wasi');
const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { createHash } = require('node:crypto');
const assert = require('node:assert/strict');
const fixture = path.resolve('docs/research/fixtures/ufdr/messages-minimal.xml');
if (process.argv[2] === '--wasi') {
  const args = ['worker', '/fixture/' + path.basename(fixture), ...process.argv.slice(3)];
  const runtime = new WASI({ version: 'preview1', returnOnExit: true, args, preopens: { '/fixture': path.dirname(fixture) } });
  const module = new WebAssembly.Module(fs.readFileSync('bin/worker.wasm'));
  process.exitCode = runtime.start(new WebAssembly.Instance(module, { wasi_snapshot_preview1: runtime.wasiImport }));
} else {
  const directory = path.dirname(fixture);
  for (const line of fs.readFileSync(path.join(directory, 'SHA256SUMS'), 'utf8').trim().split(/\r?\n/)) {
    const entry = /^([a-f0-9]{64})  (.+)$/i.exec(line);
    assert.ok(entry, 'invalid fixture checksum entry');
    const actual = createHash('sha256').update(fs.readFileSync(path.join(directory, entry[2]))).digest('hex');
    assert.equal(actual, entry[1].toLowerCase(), `fixture checksum mismatch: ${entry[2]}`);
  }
  const before = fs.readFileSync(fixture);
  const hash = createHash('sha256').update(before).digest('hex');
  const native = path.resolve('bin/worker-native' + (process.platform === 'win32' ? '.exe' : ''));
  for (const expectedHash of [hash, '0'.repeat(64)]) {
    const options = ['-input-sha256', expectedHash];
    const a = spawnSync(native, [fixture, ...options], { encoding: 'utf8' });
    const b = spawnSync(process.execPath, [__filename, '--wasi', ...options], { encoding: 'utf8' });
    assert.ifError(a.error); assert.ifError(b.error);
    if (expectedHash === hash) {
      assert.equal(a.status, 0, a.stderr); assert.equal(b.status, 0, b.stderr);
      assert.deepEqual(JSON.parse(a.stdout), JSON.parse(b.stdout));
      assert.deepEqual(JSON.parse(a.stdout), JSON.parse(fs.readFileSync(fixture.replace('.xml', '.expected.json'), 'utf8')));
    } else {
      for (const result of [a,b]) {
        assert.notEqual(result.status, 0);
        assert.equal(result.stdout, '');
        assert.match(result.stderr, /digest mismatch/);
      }
    }
  }
  assert.deepEqual(fs.readFileSync(fixture), before);
  console.log('PASS: native/preview1 WASI fixture contract, hash rejection, original bytes');
}
