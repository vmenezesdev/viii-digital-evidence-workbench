// Runs against the actual product API, optionally its native WebView2 CDP endpoint.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('../.poc-runtime/pdf/browser/node_modules/playwright');

(async () => {
  const results = process.env.VIII_PDF_UI_RESULTS;
  assert(results, 'set VIII_PDF_UI_RESULTS to an existing test results directory');
  const target = Number(process.env.VIII_PDF_UI_PAGE || 11);
  const searchable = Number(process.env.VIII_PDF_UI_SEARCHABLE || 11);
  assert(Number.isInteger(target) && target >= 1 && target <= 30000, 'invalid physical target page');
  assert(Number.isInteger(searchable) && searchable >= 1 && searchable <= 30000, 'invalid expected searchable count');
  const native = process.env.VIII_PDF_CDP;
  const browser = native
    ? await chromium.connectOverCDP(native)
    : await chromium.launch({ channel: 'chrome', headless: true });
  const context = native ? browser.contexts()[0] : await browser.newContext({ viewport: { width: 1400, height: 1100 } });
  const page = native ? context.pages()[0] : await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  try {
    if (!native) await page.goto('http://127.0.0.1:49081/pdf.html');
    if (process.env.VIII_PDF_UI_NETWORK_CONTROL) {
      const cdp = await context.newCDPSession(page);
      await cdp.send('Network.enable');
      const requests = new Set();
      const failed = new Promise(resolve => {
        cdp.on('Network.requestWillBeSent', event => { if (event.request.url === 'https://example.com/') requests.add(event.requestId); });
        cdp.on('Network.loadingFailed', event => { if (requests.has(event.requestId)) resolve(event.errorText); });
      });
      await page.evaluate(async () => { try { await fetch('https://example.com/'); } catch {} });
      const reason = await Promise.race([failed, new Promise((_, reject) => setTimeout(() => reject(Error('No network failure control observed')), 10000))]);
      assert.match(reason, /ERR_PROXY_CONNECTION_FAILED/);
      fs.writeFileSync(path.join(results, 'network-control.json'), JSON.stringify({ url: 'https://example.com/', error: reason, local_page: page.url() }, null, 2));
      await cdp.detach();
    }
    if (process.env.VIII_PDF_UI_IMPORT) {
      const start = Date.now();
      await page.locator('#file').setInputFiles(process.env.VIII_PDF_UI_IMPORT);
      const imported = page.waitForResponse(response => response.url().includes('/api/pdf/import?') && response.request().method() === 'POST');
      await page.locator('#import').click();
      assert.equal((await imported).status(), 202);
      await page.locator('#status').filter({ hasText: /Extraindo texto|OCR em andamento/ }).waitFor({ timeout: 30000 });
      await page.locator('#status').filter({ hasText: /[1-9]\d* pesquisáveis/ }).waitFor({ timeout: 90000 });
      await page.locator('#query').fill('ARARA');
      await page.locator('#search').click();
      await page.locator('#hits a').first().waitFor();
      const partial = { first_useful_search_ms: Date.now() - start, status: await page.locator('#status').innerText(), hits: await page.locator('#hits a').count() };
      assert(!partial.status.includes('Concluído'), 'first useful UI search must occur while processing continues');
      fs.writeFileSync(path.join(results, 'native-partial-search.json'), JSON.stringify(partial, null, 2));
      await page.screenshot({ path: path.join(results, 'native-partial-search.png'), fullPage: true });
    }
    await page.locator('#status').filter({ hasText: `${searchable} pesquisáveis` }).waitFor({ timeout: 360000 });
    await page.locator('#query').fill('JABUTICABA');
    await page.locator('#search').click();
    await page.locator('#hits a').filter({ hasText: `Página ${target}` }).waitFor();
    await page.locator('#ask').click();
    await page.locator('#answer').filter({ hasText: '15' }).waitFor({ timeout: 120000 });
    await page.locator('#citations a').filter({ hasText: `Página ${target}` }).click();
    assert.match(await page.locator('#viewer').getAttribute('src'), new RegExp(`#page=${target}$`));
    await page.waitForTimeout(3000);
    await page.screenshot({ path: path.join(results, native ? 'native-ui.png' : 'browser-ui.png'), fullPage: true });
    const report = { passed: true, native: Boolean(native), url: page.url(), status: await page.locator('#status').innerText(), answer: await page.locator('#answer').innerText(), citation: await page.locator('#viewer').getAttribute('src'), partial: await page.locator('#partial').innerText(), errors };
    assert.deepEqual(errors, []);
    fs.writeFileSync(path.join(results, native ? 'native-ui.json' : 'browser-ui.json'), JSON.stringify(report, null, 2));
    console.log(JSON.stringify(report));
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
