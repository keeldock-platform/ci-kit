'use strict';
// This is a native engine readiness probe, not product test coverage.
const path = require('node:path');
const {createRequire} = require('node:module');
const local = createRequire(path.join(process.cwd(), 'package.json'));
(async () => {
  const version = local('playwright/package.json').version;
  if (version !== '1.65.1') throw new Error('version');
  const playwright = local('playwright');
  for (const engine of ['chromium', 'firefox', 'webkit']) {
    const browser = await playwright[engine].launch({headless: true});
    await browser.close();
  }
  process.stdout.write(version);
})().catch(() => { process.exitCode = 1; });
