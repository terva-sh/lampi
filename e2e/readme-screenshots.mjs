// Run with node e2e/readme-screenshots.mjs /absolute/path/to/playwright/index.mjs
// Regenerates docs/images/dashboard-*.png from the synthetic smoketest
// fixture, so the images never show a real lake. See e2e/README.md.
import {spawn} from 'node:child_process';
import {mkdtemp, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {pathToFileURL} from 'node:url';
const {chromium} = await import(pathToFileURL(process.argv[2]).href);
const out = join(process.cwd(), 'docs', 'images');
const buildDir = await mkdtemp(join(tmpdir(), 'lampi-screenshot-build-'));
const binary = join(buildDir, 'fixture');
const build = spawn('go', ['build', '-buildvcs=false', '-o', binary, './internal/web/smoketest'], {stdio: 'inherit'});
await new Promise((resolve, reject) => build.on('exit', code => code === 0 ? resolve() : reject(new Error('fixture build failed'))));
const proc = spawn(binary, [], {stdio: ['ignore', 'pipe', 'inherit']});
let browser;
try {
  const live = await new Promise((resolve, reject) => {
    let buf = '';
    proc.stdout.on('data', d => { buf += d; if (buf.includes('\n')) resolve(JSON.parse(buf.split('\n')[0])); });
    proc.on('exit', code => reject(new Error(`fixture exited ${code}`)));
  });
  browser = await chromium.launch({headless: true});
  const context = await browser.newContext({ignoreHTTPSErrors: true, viewport: {width: 1280, height: 800}, deviceScaleFactor: 2});
  const page = await context.newPage();
  await page.goto(live.url);
  await page.getByRole('heading', {name: 'A view into your lake.'}).waitFor();
  await page.screenshot({path: join(out, 'dashboard-overview.png')});
  // The fixture's first sessions carry markup to test escaping. Pick one
  // with a plain title.
  await page.goto(live.url + '/sessions?state=ready');
  const names = page.locator('.session-name');
  const count = await names.count();
  for (let i = 0; i < count; i++) {
    if (!(await names.nth(i).innerText()).includes('<')) { await names.nth(i).click(); break; }
  }
  await page.getByRole('link', {name: 'Read transcript →'}).click();
  await page.waitForLoadState('networkidle');
  await page.screenshot({path: join(out, 'dashboard-transcript.png')});
  console.log(`wrote ${out}/dashboard-overview.png and dashboard-transcript.png`);
} finally {
  await browser?.close();
  proc.kill();
  await rm(buildDir, {recursive: true, force: true});
}
