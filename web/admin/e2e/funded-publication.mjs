import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1'); assert.equal(origin.pathname, '/');
  assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_STRATEGY_ID: id, MUSKDEER_BROWSER_TEMPLATE: template, MUSKDEER_BROWSER_PUBLICATION_PHASE: phase, MUSKDEER_BROWSER_SOURCE: source, MUSKDEER_BROWSER_VERSION: version, MUSKDEER_BROWSER_NOTIONAL: notional, MUSKDEER_BROWSER_PARAMETER_BUDGET: budget } = process.env;
  assert.ok(password && artifacts && /^[1-9][0-9]*$/.test(id ?? '') && ['grid', 'market_maker'].includes(template) && ['running', 'paused', 'stopped'].includes(phase) && ['binance', 'okx'].includes(source) && /^[2-9]$/.test(version ?? '') && /^[0-9]+(\.[0-9]+)?$/.test(notional ?? '') && /^[0-9]+(\.[0-9]+)?$/.test(budget ?? ''));
  await mkdir(artifacts, { recursive: true, mode: 0o700 });
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    const page = await context.newPage(); page.setDefaultTimeout(10000); const errors = [];
    page.on('pageerror', error => errors.push(error.name));
    stage = 'actual-login'; await page.goto(new URL(`/strategies/${id}`, origin).href);
    await page.getByLabel('用户名', { exact: true }).fill('control-operator'); await page.getByLabel('密码', { exact: true }).fill(password);
    await page.getByRole('button', { name: '登录', exact: true }).click();
    stage = `${phase}-open`; await page.getByRole('button', { name: '发布参数与换源', exact: true }).click();
    const form = page.getByRole('region', { name: '发布参数与换源', exact: true });
    await form.getByLabel('新版本行情来源', { exact: true }).locator(`option[value="${source}"]`).waitFor({ state: 'attached' });
    await form.getByLabel('新版本行情来源', { exact: true }).selectOption(source);
    await form.getByLabel(template === 'grid' ? '单笔名义额（报价资产）' : '单档名义额（报价资产）', { exact: true }).fill(notional);
    await form.getByLabel(template === 'grid' ? '总预算（报价资产）' : '单侧预算（报价资产）', { exact: true }).fill(budget);
    await form.getByLabel('发布原因', { exact: true }).fill(`browser ${phase} funded publication`);
    stage = `${phase}-publish`; await form.getByRole('button', { name: '确认发布新版本', exact: true }).click();
    await form.getByText(`已记录请求版本 v${version}。请分别核对请求配置与生效配置，发布收据不代表已完成切换。`).waitFor();
    await page.screenshot({ path: `${artifacts}/funded-${template}-${phase}.png`, fullPage: true });
    assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    await context.close();
  } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Funded publication browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
