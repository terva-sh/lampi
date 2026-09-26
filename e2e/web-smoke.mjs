// Run with mise exec -- node e2e/web-smoke.mjs /absolute/path/to/playwright/index.mjs
// Playwright is a test-only dependency installed outside this Go repository.
import {spawn} from 'node:child_process';
import {mkdtemp, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {pathToFileURL} from 'node:url';
import assert from 'node:assert/strict';
const {chromium} = await import(pathToFileURL(process.argv[2]).href);
const artifacts = await mkdtemp(join(tmpdir(), 'lampi-browser-evidence-'));
const buildDir = await mkdtemp(join(tmpdir(), 'lampi-browser-build-'));
const binary = join(buildDir, 'fixture');
const build = spawn('go', ['build', '-buildvcs=false', '-o', binary, './internal/web/smoketest'], {stdio: 'inherit'});
await new Promise((resolve, reject) => build.on('exit', code => code === 0 ? resolve() : reject(new Error('fixture build failed'))));
const processes = [];
async function fixture(args = []) {
  const proc = spawn(binary, args, {stdio: ['ignore', 'pipe', 'inherit']});
  processes.push(proc);
  return await new Promise((resolve, reject) => {
    let out = '';
    const timer = setTimeout(() => reject(new Error('fixture startup timed out')), 30000);
    proc.stdout.on('data', data => { out += data; if (out.includes('\n')) { clearTimeout(timer); resolve(JSON.parse(out.split('\n')[0])); } });
    proc.on('exit', code => {clearTimeout(timer); reject(new Error(`fixture exited ${code}`));});
  });
}
let browser;
try {
  browser = await chromium.launch({headless: true});
  const live = await fixture();
  const context = await browser.newContext({ignoreHTTPSErrors: true, viewport: {width: 1440, height: 1100}});
  const page = await context.newPage();
  page.setDefaultTimeout(10000);
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.goto(live.url);
  await page.getByRole('heading', {name: 'A view into your lake.'}).waitFor();
  assert.equal(await page.locator('.stats article').first().locator('strong').innerText(), '123');
  assert.equal(await page.evaluate(() => window.owned), undefined);
  await page.screenshot({path: join(artifacts, 'overview-desktop.png'), fullPage: true});
  await page.getByRole('navigation', {name: 'Main navigation'}).getByRole('link', {name: 'Sessions', exact: true}).click();
  assert.equal(await page.locator('tbody tr').count(), 50);
  await page.getByRole('link', {name: 'Next page'}).click();
  assert.equal(await page.locator('body').getAttribute('data-poll'), 'false');
  assert.equal(await page.locator('select[name=harness]').count(), 1, (await page.locator('body').innerText()).slice(0,1000));
  await page.getByLabel('Harness', {exact: true}).selectOption('codex');
  await page.getByRole('button', {name: 'Apply filters'}).click();
  assert.equal(await page.locator('tbody tr').count(), 21);
  assert.match(await page.locator('tbody tr').first().innerText(), /codex/);
  await page.getByRole('link', {name: 'Clear', exact: true}).click();
  await page.locator('.session-name').first().click();
  await page.getByRole('heading', {name: 'Session details', exact: true}).waitFor();
  await page.getByRole('link', {name: 'Provenance', exact: true}).click();
  assert.match(await page.locator('tbody').innerText(), /synthetic-machine/);
  await page.getByRole('navigation', {name: 'Main navigation'}).getByRole('link', {name: 'Conflicts', exact: true}).click();
  assert.ok((await page.locator('tbody tr').count()) > 0);
  assert.match(await page.locator('tbody').innerText(), /Head retained/);
  await page.goto(live.url);
  await page.clock.install();
  let refreshes = 0;
  page.on('request', r => {if (r.headers()['x-lampi-refresh']) refreshes++;});
  await page.getByRole('button', {name: 'Refresh now'}).click();
  await page.waitForFunction(() => document.querySelector('#refresh-status').textContent.startsWith('Up to date'));
  await page.evaluate(() => {Object.defineProperty(document, 'hidden', {configurable: true, value: true}); document.dispatchEvent(new Event('visibilitychange'));});
  const before = refreshes;
  await page.clock.fastForward(30000);
  assert.equal(refreshes, before, 'hidden tab polled');
  await page.evaluate(() => {Object.defineProperty(document, 'hidden', {configurable: true, value: false}); document.dispatchEvent(new Event('visibilitychange'));});
  const response = page.waitForResponse(r => r.request().headers()['x-lampi-refresh'] === '1');
  await page.clock.fastForward(25000); await response;
  const failRefresh = async route => {
    if (route.request().headers()['x-lampi-refresh']) await route.fulfill({status: 503, contentType: 'text/plain', body: 'synthetic read failure'});
    else await route.continue();
  };
  await page.route(live.url + '/', failRefresh);
  await page.getByRole('button', {name: 'Refresh now'}).click();
  await page.waitForFunction(() => document.querySelector('#refresh-status').textContent.includes('Refresh failed'));
  await page.unroute(live.url + '/', failRefresh);
  await page.getByRole('button', {name: 'Refresh now'}).click();
  await page.waitForFunction(() => document.querySelector('#refresh-status').textContent.startsWith('Up to date'));
  await page.clock.resume();
  await page.setViewportSize({width: 390, height: 844});
  assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile page overflows');
  await page.screenshot({path: join(artifacts, 'overview-mobile.png'), fullPage: true});
  await page.keyboard.press('Tab');
  assert.ok(await page.evaluate(() => document.activeElement.tagName !== 'BODY'), 'keyboard focus absent');
  // Follow POST logout only as far as its redirect; the fake IdP automatically
  // signs in, so following the next navigation would create a new session.
  const token = await page.locator('input[name=csrf]').inputValue();
  const logout = await context.request.post(live.url + '/auth/oidc/logout', {form: {csrf: token}, headers: {Origin: live.url}, maxRedirects: 0});
  assert.equal(logout.status(), 303);
  assert.equal((await context.request.get(live.url + '/api/web/v1/overview')).status(), 401);
  await page.getByRole('button', {name: 'Refresh now'}).click();
  await page.waitForFunction(() => document.querySelector('#refresh-status').textContent.includes('session has ended'));
  assert.equal(await page.getByRole('link', {name: 'Sign in again'}).isVisible(), true);
  // No-JS pages remain navigable and filterable.
  const noJS = await browser.newContext({ignoreHTTPSErrors: true, javaScriptEnabled: false});
  const basic = await noJS.newPage(); await basic.goto(live.url + '/sessions');
  await basic.getByLabel('Harness', {exact: true}).selectOption('claude');
  await basic.getByRole('button', {name: 'Apply filters'}).click();
  assert.equal(await basic.locator('tbody tr').count(), 21);
  const denied = await fixture(['--deny']);
  const denyPage = await context.newPage(); await denyPage.goto(denied.url);
  await denyPage.getByRole('heading', {name: /no lake viewer access/}).waitFor();
  const empty = await fixture(['--empty']);
  const emptyPage = await context.newPage(); await emptyPage.goto(empty.url);
  await emptyPage.getByRole('heading', {name: 'No sessions to show'}).waitFor();
  assert.deepEqual(errors, []);
  console.log(JSON.stringify({result: 'passed', evidence: artifacts, checks: ['OIDC login', '123 sessions', 'pagination', 'filters', 'details/provenance', 'conflicts', 'refresh/visibility/error/recovery/expiry', 'mobile', 'keyboard', 'logout', 'no-JS', 'denied group', 'empty lake']}));
} finally {
  if (browser) await browser.close();
  await Promise.all(processes.map(proc => new Promise(resolve => {if (proc.exitCode !== null) return resolve(); proc.once('exit', resolve); proc.kill('SIGTERM');})));
  await rm(buildDir, {recursive: true, force: true});
}
