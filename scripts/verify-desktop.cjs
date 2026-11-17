// Check the embedded UI's state transitions without desktop infrastructure.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const elements = new Map();
const createElement = tagName => ({
  tagName, textContent: '', value: '', children: [],
  replaceChildren() { this.children = []; },
  append(child) { this.children.push(child); },
});
const element = id => {
  if (!elements.has(id)) elements.set(id, createElement());
  return elements.get(id);
};
const storage = new Map();
const context = vm.createContext({
  document: { getElementById: element, createElement },
  localStorage: {
    getItem: key => storage.get(key),
    setItem: (key, value) => storage.set(key, value),
    removeItem: key => storage.delete(key),
  },
  URLSearchParams,
});
const html = fs.readFileSync('cmd/desktop/assets/index.html', 'utf8');
vm.runInContext(html.match(/<script>([\s\S]*)<\/script>/)[1], context);
const record = id => ({
  evidence_id: id, finished: 'done', original_xml: `<project>${id}</project>`,
  result: { messages: [{ id, body: id }] },
});
const show = value => {
  context.next = value;
  return vm.runInContext('show(next)', context);
};
const select = id => {
  const button = element('messages').children[0].children[0];
  assert.equal(button.tagName, 'button', 'message selection needs a native keyboard control');
  assert.equal(button.type, 'button');
  button.onclick();
  assert.equal(element('source').textContent, record(id).original_xml);
  assert.equal(JSON.parse(element('selected').textContent).id, id);
};

(async () => {
  await show(Promise.resolve(record('A')));
  select('A');
  let resolve;
  const pending = show(new Promise(done => { resolve = done; }));
  assert.equal(element('source').textContent, '');
  assert.equal(element('selected').textContent, '');
  assert.equal(element('messages').children.length, 0);
  resolve(record('B'));
  await pending;
  assert.match(element('status').textContent, /Evidence B/);
  assert.equal(element('source').textContent, '');
  assert.equal(element('selected').textContent, '');
  select('B');
  await show(Promise.reject(Error('import failed')));
  assert.equal(element('source').textContent, '');
  assert.equal(element('selected').textContent, '');
  element('search').oninput();
  assert.equal(element('messages').children.length, 0);
  console.log('PASS: evidence transitions, failed imports and native message controls');
})().catch(error => { console.error(error); process.exitCode = 1; });
