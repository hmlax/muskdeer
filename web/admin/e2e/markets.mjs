import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1'); assert.equal(origin.pathname, '/'); assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_MARKET_PHASE: phase, MUSKDEER_BROWSER_MARKET_BINANCE: binance, MUSKDEER_BROWSER_MARKET_OKX: okx, MUSKDEER_BROWSER_MARKET_EMPTY: empty, MUSKDEER_BROWSER_MARKET_INSTRUMENT: instrument } = process.env;
  assert.ok(password && artifacts && ['healthy', 'degraded'].includes(phase) && [binance, okx, empty, instrument].every(id => /^[1-9][0-9]*$/.test(id ?? '')));
  await mkdir(artifacts, { recursive: true, mode: 0o700 }); const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    const page = await context.newPage(); page.setDefaultTimeout(8000); const errors = []; const writes = []; const reads = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { const path = new URL(request.url()).pathname; if (request.method() !== 'GET' && !path.startsWith('/api/v1/auth/')) writes.push(path); if (request.method() === 'GET' && path.startsWith('/api/v1/markets')) reads.push(request.url()); });
    const form = page.getByRole('form', { name: '行情筛选', exact: true }); const table = page.getByRole('region', { name: '行情列表', exact: true });
    const current = page.getByRole('region', { name: '当前行情读数', exact: true });
    async function field(region, label, value) { const cell = region.getByText(label, { exact: true }).locator('..').locator('dd'); await cell.filter({ hasText: value }).waitFor(); assert.equal(await cell.textContent(), value); }
    async function listReady() { await table.waitFor(); await page.locator('.market-workspace[aria-busy="false"]').waitFor(); }
    async function query(mode, source, product, limit) {
      const response = page.waitForResponse(reply => { const u = new URL(reply.url()); return u.pathname === '/api/v1/markets' && reply.status() === 200 && u.searchParams.get('mode') === mode && (u.searchParams.get('source') ?? '') === source && (u.searchParams.get('instrument_id') ?? '') === product && u.searchParams.get('limit') === limit; });
      await form.getByLabel('行情模式', { exact: true }).selectOption(mode); await form.getByLabel('行情来源', { exact: true }).selectOption(source); await form.getByLabel('交易对 ID（精确）', { exact: true }).fill(product); await form.getByLabel('每页行情', { exact: true }).selectOption(limit); await form.getByRole('button', { name: '查询行情', exact: true }).click(); await response; await page.locator('.market-workspace[aria-busy="false"]').waitFor();
    }
    stage = `${phase}-actual-login`; await page.goto(new URL(phase === 'healthy' ? '/markets?mode=REPLAY' : `/markets/${binance}`, origin).href);
    if (phase === 'healthy') await page.route('**/api/v1/markets?*', route => route.abort());
    await page.getByLabel('用户名', { exact: true }).fill('control-operator'); await page.getByLabel('密码', { exact: true }).fill(password); await page.getByRole('button', { name: '登录', exact: true }).click();
    if (phase === 'healthy') {
      stage = 'actual-network-read-retry'; await page.getByRole('heading', { name: '行情监控', exact: true }).waitFor(); await page.getByText('暂时无法读取行情监控，请稍后重试。', { exact: true }).waitFor();
      await page.unroute('**/api/v1/markets?*'); await page.getByRole('button', { name: '重试', exact: true }).click(); await listReady(); assert.equal(await table.locator('tbody tr').count(), 3);
      await table.locator('td').filter({ hasText: '99.9999999999999999995' }).first().waitFor(); await page.screenshot({ path: `${artifacts}/market-list-desktop.png`, fullPage: true });
      stage = 'actual-feed-pages'; await query('REPLAY', '', '', '1'); await listReady(); await page.getByRole('link', { name: `查看行情 ${binance}`, exact: true }).waitFor();
      await page.getByRole('button', { name: '下一页行情', exact: true }).click(); await page.getByRole('link', { name: `查看行情 ${okx}`, exact: true }).waitFor();
      await page.getByRole('button', { name: '下一页行情', exact: true }).click(); await page.getByRole('link', { name: `查看行情 ${empty}`, exact: true }).waitFor(); await table.getByText('尚无快照', { exact: true }).waitFor(); assert.equal(await page.getByRole('button', { name: '下一页行情', exact: true }).isDisabled(), true);
      await page.getByRole('button', { name: '上一页行情', exact: true }).click(); await page.getByRole('link', { name: `查看行情 ${okx}`, exact: true }).waitFor();
      stage = 'actual-exact-source-and-product'; await query('REPLAY', 'okx', instrument, '20'); await listReady(); assert.equal(await table.locator('tbody tr').count(), 1); await page.getByRole('link', { name: `查看行情 ${okx}`, exact: true }).waitFor();
      await page.reload(); await listReady(); assert.equal(await form.getByLabel('行情来源', { exact: true }).inputValue(), 'okx'); assert.equal(await form.getByLabel('交易对 ID（精确）', { exact: true }).inputValue(), instrument);
      stage = 'invalid-local-filter-no-request'; await form.getByLabel('交易对 ID（精确）', { exact: true }).fill('01'); const before = reads.length; await form.getByRole('button', { name: '查询行情', exact: true }).click(); await page.getByText('交易对 ID 必须是规范正整数；请选择有效模式与来源。', { exact: true }).waitFor(); assert.equal(reads.length, before);
      stage = 'actual-live-empty-not-replay'; await query('LIVE', '', '', '20'); await page.getByRole('heading', { name: '没有匹配的行情绑定', exact: true }).waitFor(); assert.equal(await table.count(), 0);
      stage = 'actual-unpublished-detail'; await page.goto(new URL(`/markets/${empty}`, origin).href); await page.getByText('绑定已配置，但尚无可见快照；这不表示健康，也不会自动使用其他来源。', { exact: true }).waitFor(); assert.equal(await current.count(), 0);
      stage = 'actual-precision-and-deep-refresh'; await page.goto(new URL(`/markets/${binance}`, origin).href); await field(current, '采样中价', '99.9999999999999999995'); await field(current, '采样买一价', '99.999999999999999999');
      await page.reload(); await field(current, '采样中价', '99.9999999999999999995');
      stage = 'local-deadline-no-hidden-poll'; const readCount = reads.length; await current.getByText('本次读数已过期', { exact: true }).waitFor(); assert.equal(reads.length, readCount);
      await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/market-detail-mobile.png`, fullPage: true });
      const path = new URL(`/api/v1/markets/${binance}`, origin).href;
      stage = 'protocol-number-no-stale-detail'; await page.route(path, async route => { const response = await route.fetch(); const body = await response.json(); body.market.snapshot.reading.sequence = 1; await route.fulfill({ response, json: body }); });
      await page.getByRole('button', { name: '刷新行情', exact: true }).click(); await page.getByText('暂时无法读取行情监控，请稍后重试。', { exact: true }).waitFor(); assert.equal(await current.count(), 0); await page.unroute(path); await page.getByRole('button', { name: '重试', exact: true }).click(); await field(current, '采样中价', '99.9999999999999999995');
      stage = 'forbidden-presentation'; await page.route(path, route => route.fulfill({ status: 403, contentType: 'application/json', json: { code: 'FORBIDDEN' } })); await page.getByRole('button', { name: '刷新行情', exact: true }).click(); await page.getByText('当前账户无权读取行情监控。', { exact: true }).waitFor(); assert.equal(await current.count(), 0); await page.unroute(path); await page.getByRole('button', { name: '重试', exact: true }).click(); await field(current, '采样中价', '99.9999999999999999995');
      stage = 'actual-missing-feed'; await page.goto(new URL('/markets/9223372036854775807', origin).href); await page.getByText('未找到此行情绑定或交易对。', { exact: true }).waitFor();
    } else {
      stage = 'actual-selected-publisher-shutdown'; await field(current, '读取时质量', '不可用'); await field(current, '采样中价', '—'); await field(current, '读取时发布者有效', '否'); await page.screenshot({ path: `${artifacts}/market-degraded-desktop.png`, fullPage: true });
      stage = 'other-real-source-independent'; await page.goto(new URL(`/markets/${okx}`, origin).href); await field(current, '读取时质量', '健康'); await field(current, '采样中价', '200');
      await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/market-okx-mobile.png`, fullPage: true });
      stage = 'actual-no-source-substitution'; await page.goto(new URL(`/markets?mode=REPLAY&source=binance&instrument_id=${instrument}`, origin).href); await listReady(); assert.equal(await table.locator('tbody tr').count(), 1); await table.getByText('不可用', { exact: true }).waitFor(); assert.equal(await page.getByRole('link', { name: `查看行情 ${okx}`, exact: true }).count(), 0);
      stage = 'actual-revoked-cookie'; const cookies = await context.cookies(origin.href); const csrf = cookies.find(cookie => cookie.name === 'muskdeer_csrf'); assert.ok(csrf);
      const logout = await context.request.post(new URL('/api/v1/auth/logout', origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': csrf.value } }); assert.equal(logout.status(), 204); await context.addCookies(cookies);
      await page.getByRole('button', { name: '刷新行情', exact: true }).click(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor(); assert.equal(await table.count(), 0);
    }
    assert.deepEqual(writes, []); assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0); await context.close();
  } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Market browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
