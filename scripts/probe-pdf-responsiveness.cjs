const assert = require('node:assert/strict');
const fs = require('node:fs');
const { chromium } = require('../.poc-runtime/pdf/browser/node_modules/playwright');

// Repeated interaction with a completed product index while WASI jobs run.
(async () => {
  const [url, stop, output] = process.argv.slice(2);
  assert(url && stop && output, 'usage: probe url stop-file output.jsonl');
  const browser = await chromium.launch({ channel: 'chrome', headless: true });
  try {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(url);
    let expected = Date.now();
    while (!fs.existsSync(stop)) {
      const start = Date.now();
      const response = page.waitForResponse(r => r.url().includes('/api/pdf/search?'));
      await page.locator('#query').fill('ARARA');
      await page.locator('#search').click();
      assert.equal((await response).status(), 200);
      await page.locator('#hits a').first().waitFor();
      assert.deepEqual(errors, []);
      fs.appendFileSync(output, JSON.stringify({ at: new Date().toISOString(), ui_search_ms: Date.now() - start, scheduler_delay_ms: Math.max(0, start - expected), hits: await page.locator('#hits a').count() }) + '\n');
      expected = Date.now() + 1000;
      await new Promise(resolve => setTimeout(resolve, 1000));
    }
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
