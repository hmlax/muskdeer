import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1'); assert.equal(origin.pathname, '/');
  assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_ORDER_STRATEGY: strategy, MUSKDEER_BROWSER_ORDER_LIVE: live, MUSKDEER_BROWSER_ORDER_UNSENT: unsent, MUSKDEER_BROWSER_ORDER_UNKNOWN: unknown } = process.env;
  assert.ok(password && artifacts && [strategy, live, unsent, unknown].every(value => /^[1-9][0-9]*$/.test(value ?? '')));
  await mkdir(artifacts, { recursive: true, mode: 0o700 }); stage = 'browser-launch'; const browser = await chromium.launch({ headless: true });
  try {
    stage = 'browser-context'; const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    stage = 'browser-page'; const page = await context.newPage(); page.setDefaultTimeout(8000); const errors = []; const writes = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { const path = new URL(request.url()).pathname; if (request.method() !== 'GET' && !path.startsWith('/api/v1/auth/')) writes.push(path); });
    const panel = page.getByRole('region', { name: '订单与核心事实', exact: true });
    const rows = panel.getByRole('region', { name: '订单列表', exact: true }).locator('tbody tr');
    const detail = panel.getByRole('region', { name: '订单详情', exact: true });
    async function field(name, value) { const cell = detail.getByText(name, { exact: true }).locator('..').locator('dd'); await cell.filter({ hasText: value }).waitFor(); assert.equal(await cell.textContent(), value); }
    stage = 'actual-viewer-login'; await page.goto(new URL(`/strategies/${strategy}`, origin).href);
    await page.getByLabel('用户名', { exact: true }).fill('orders-viewer'); await page.getByLabel('密码', { exact: true }).fill(password); await page.getByRole('button', { name: '登录', exact: true }).click();
    await panel.getByRole('button', { name: '查看订单', exact: true }).click(); await panel.getByRole('button', { name: `查看订单 ${live}`, exact: true }).waitFor();
    assert.equal(await rows.count(), 3); assert.ok((await rows.first().textContent()).includes('Binance'));
    stage = 'actual-independent-observations'; await panel.getByRole('button', { name: `查看订单 ${live}`, exact: true }).click();
    await field('OMS 已确认成交', '0'); await field('核心成交数量', '1'); await field('OMS 名义额预留', '200'); await field('冻结金额', '100.1');
    await field('历史配置与来源', 'v1 · Binance'); await field('OMS 状态 / 撤单', 'LIVE / NONE'); await field('核心状态 / 撤单', 'PARTIALLY_FILLED / NONE');
    await page.screenshot({ path: `${artifacts}/orders-desktop.png`, fullPage: true });
    stage = 'actual-keyset-pagination'; await panel.getByLabel('每页订单', { exact: true }).selectOption('1');
    await panel.getByRole('button', { name: `查看订单 ${live}`, exact: true }).waitFor(); assert.equal(await rows.count(), 1);
    await panel.getByRole('button', { name: '下一页订单', exact: true }).click(); await panel.getByRole('button', { name: `查看订单 ${unsent}`, exact: true }).click();
    await detail.getByText('尚未观察到核心记录；这不能证明订单未受理。', { exact: true }).waitFor(); await field('OMS 名义额预留', '10');
    await panel.getByRole('button', { name: '下一页订单', exact: true }).click(); await panel.getByRole('button', { name: `查看订单 ${unknown}`, exact: true }).click();
    await field('OMS 状态 / 撤单', 'UNKNOWN / NONE'); await field('核心状态 / 撤单', 'LIVE / NONE'); await field('已确认核心 ID / 修订', '0 / 0');
    assert.equal(await panel.getByRole('button', { name: '下一页订单', exact: true }).isDisabled(), true);
    stage = 'actual-state-filter-and-empty'; await panel.getByLabel('OMS 状态', { exact: true }).selectOption('FILLED'); await panel.getByText('没有匹配的订单', { exact: true }).waitFor();
    await panel.getByLabel('OMS 状态', { exact: true }).selectOption('UNKNOWN'); await panel.getByRole('checkbox').check(); await panel.getByRole('button', { name: `查看订单 ${unknown}`, exact: true }).waitFor();
    stage = 'protocol-failure-clears-stale-data'; await page.route(`**/api/v1/strategies/${strategy}/orders?*`, async route => { const response = await route.fetch(); const body = await response.json(); body.orders[0].reserved_notional = 10; await route.fulfill({ response, json: body }); });
    await panel.getByRole('button', { name: '刷新订单', exact: true }).click(); await panel.getByText('暂时无法读取订单，请稍后重试。', { exact: true }).waitFor(); assert.equal(await rows.count(), 0);
    await page.unrouteAll({ behavior: 'wait' }); await panel.getByRole('button', { name: '重试订单读取', exact: true }).click(); await panel.getByRole('button', { name: `查看订单 ${unknown}`, exact: true }).click(); await field('冻结金额', '10.01');
    stage = 'mobile'; await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/orders-mobile.png`, fullPage: true });
    stage = 'actual-revoked-cookie'; const cookies = await context.cookies(origin.href); const csrf = cookies.find(cookie => cookie.name === 'muskdeer_csrf'); assert.ok(csrf);
    const logout = await context.request.post(new URL('/api/v1/auth/logout', origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': csrf.value } }); assert.equal(logout.status(), 204); await context.addCookies(cookies);
    await panel.getByRole('button', { name: '刷新订单', exact: true }).click(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor();
    assert.equal(await panel.count(), 0); assert.deepEqual(writes, []); assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    await context.close();
  } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Orders browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
