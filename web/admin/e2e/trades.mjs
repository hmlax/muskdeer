import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1'); assert.equal(origin.pathname, '/');
  assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_TRADE_STRATEGY: strategy, MUSKDEER_BROWSER_TRADE_OTHER: other, MUSKDEER_BROWSER_TRADE_BUY: buy, MUSKDEER_BROWSER_TRADE_SELL: sell, MUSKDEER_BROWSER_TRADE_ORDER: order } = process.env;
  assert.ok(password && artifacts && [strategy, other, buy, sell, order].every(value => /^[1-9][0-9]*$/.test(value ?? '')));
  await mkdir(artifacts, { recursive: true, mode: 0o700 }); stage = 'browser-launch'; const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    const page = await context.newPage(); page.setDefaultTimeout(8000); const errors = []; const writes = []; const reads = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { const path = new URL(request.url()).pathname; if (request.method() !== 'GET' && !path.startsWith('/api/v1/auth/')) writes.push(path); if (request.method() === 'GET' && path.includes('/trades')) reads.push(path); });
    const path = `/strategies/${strategy}/trades`; const api = `/api/v1${path}`;
    const detail = page.getByRole('region', { name: '成交详情', exact: true });
    const rows = page.getByRole('region', { name: '成交列表', exact: true }).locator('tbody tr');
    async function field(name, value) { const cell = detail.getByText(name, { exact: true }).locator('..').locator('dd'); await cell.filter({ hasText: value }).waitFor(); assert.equal(await cell.textContent(), value); }
    async function apply(orderID, side, limit = '20') { await page.getByLabel('原 OMS 订单 ID', { exact: true }).fill(orderID); await page.getByLabel('成交方向', { exact: true }).selectOption(side); await page.getByLabel('每页成交', { exact: true }).selectOption(limit); await page.getByRole('button', { name: '筛选成交', exact: true }).click(); }
    stage = 'actual-viewer-deep-login'; await page.goto(new URL(`${path}/${buy}`, origin).href);
    await page.getByLabel('用户名', { exact: true }).fill('trades-viewer'); await page.getByLabel('密码', { exact: true }).fill(password); await page.getByRole('button', { name: '登录', exact: true }).click();
    await field('核心成交 ID', buy); await field('原 OMS 订单 ID', order); await field('价格', '99'); await field('成交数量', '1'); await field('手续费', '0.099'); await field('历史配置与来源', 'v1 · Binance'); await field('持仓事实收录', '尚未收录');
    assert.equal(new URL(page.url()).pathname, `${path}/${buy}`);
    stage = 'actual-strategy-navigation'; await page.goto(new URL(`/strategies/${strategy}`, origin).href); await page.getByRole('link', { name: '查看策略成交 ↗', exact: true }).click();
    await page.getByRole('link', { name: `查看成交 ${sell}`, exact: true }).waitFor(); assert.equal(await rows.count(), 2);
    assert.ok((await rows.first().textContent()).includes('尚未收录')); assert.ok((await rows.last().textContent()).includes('已收录成交事实'));
    await page.screenshot({ path: `${artifacts}/trades-desktop.png`, fullPage: true });
    stage = 'actual-one-row-page'; await apply('', '', '1'); await rows.nth(1).waitFor({ state: 'detached' }); await page.getByRole('link', { name: `查看成交 ${buy}`, exact: true }).waitFor(); assert.equal(await rows.count(), 1);
    stage = 'actual-cursor-next';
    await page.getByRole('button', { name: '下一页成交', exact: true }).click(); await page.getByRole('link', { name: `查看成交 ${sell}`, exact: true }).waitFor(); assert.equal(await page.getByRole('button', { name: '下一页成交', exact: true }).isDisabled(), true);
    await page.getByRole('button', { name: '上一页成交', exact: true }).click(); await page.getByRole('link', { name: `查看成交 ${buy}`, exact: true }).waitFor();
    stage = 'actual-order-side-filter'; await apply(order, 'SELL'); await page.getByText('没有匹配的成交', { exact: true }).waitFor();
    const beforeInvalid = reads.length; await apply('01', 'BUY'); await page.getByText('订单 ID 需为规范正整数，或留空查询全部。', { exact: true }).waitFor(); assert.equal(reads.length, beforeInvalid);
    await apply('', 'SELL'); await page.getByRole('link', { name: `查看成交 ${sell}`, exact: true }).click(); await field('持仓事实收录', '已收录成交事实'); await field('手续费', '0.101');
    stage = 'actual-deep-refresh-mobile'; await page.reload(); await field('核心成交 ID', sell); await field('持仓事实收录', '已收录成交事实');
    await detail.getByText('已收录成交事实不表示持仓完整或已对账。记录时间是数据库落库时间，不是交易所执行时间。', { exact: true }).waitFor();
    stage = 'protocol-independent-node-recording-clocks'; let earlier = '';
    await page.route(`**${api}/${sell}`, async route => { const response = await route.fetch(); const body = await response.json(); earlier = (BigInt(body.trade.recorded_at_ms) - 1000n).toString(); body.trade.portfolio_fact.recorded_at_ms = earlier; await route.fulfill({ response, json: body }); });
    const skewed = page.waitForResponse(response => new URL(response.url()).pathname === `${api}/${sell}`);
    await page.getByRole('button', { name: '刷新成交', exact: true }).click(); await skewed; await field('收录时间（UTC）', new Date(Number(earlier)).toISOString());
    await page.unroute(`**${api}/${sell}`); await page.getByRole('button', { name: '刷新成交', exact: true }).click(); await field('持仓事实收录', '已收录成交事实');
    await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/trades-mobile.png`, fullPage: true });
    stage = 'protocol-failure-clears-facts'; await page.goto(new URL(path, origin).href); await page.getByRole('link', { name: `查看成交 ${buy}`, exact: true }).waitFor();
    await page.route(`**${api}?*`, async route => { const response = await route.fetch(); const body = await response.json(); body.trades[0].fee_amount = 0.099; await route.fulfill({ response, json: body }); });
    await page.getByRole('button', { name: '刷新成交', exact: true }).click(); await page.getByText('暂时无法读取成交，请稍后重试。', { exact: true }).waitFor(); assert.equal(await rows.count(), 0);
    await page.unroute(`**${api}?*`); await page.getByRole('button', { name: '重试成交读取', exact: true }).click(); await page.getByRole('link', { name: `查看成交 ${buy}`, exact: true }).waitFor();
    stage = 'protocol-exact-strings-and-original-absence'; const probe = '<img src=x onerror="window.__tradeProbe=1">';
    await page.route(`**${api}/${buy}`, async route => { const response = await route.fetch(); const body = await response.json(); Object.assign(body.trade, { price: '100.000000000000000001', quantity: '2', quote_notional: '200.000000000000000002', fee_amount: '0.200000000000000000002', core_order_id: '9007199254740996', trade_id: probe }); delete body.trade.order_id; await route.fulfill({ response, json: body }); });
    await page.getByRole('link', { name: `查看成交 ${buy}`, exact: true }).click(); await field('价格', '100.000000000000000001'); await field('名义额', '200.000000000000000002'); await field('手续费', '0.200000000000000000002'); await field('核心订单 ID', '9007199254740996'); await field('原 OMS 订单 ID', '原始提交未关联'); await field('交易流水 ID', probe);
    assert.equal(await page.evaluate(() => window.__tradeProbe), undefined); assert.equal(await detail.locator('img').count(), 0); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/trades-precision-mobile.png`, fullPage: true });
    await page.unroute(`**${api}/${buy}`); await page.getByRole('button', { name: '刷新成交', exact: true }).click(); await field('价格', '99'); await field('原 OMS 订单 ID', order);
    stage = 'actual-empty-sibling'; await page.goto(new URL(`/strategies/${other}/trades`, origin).href); await page.getByText('没有匹配的成交', { exact: true }).waitFor();
    stage = 'permission-failure-presentation'; await page.goto(new URL(path, origin).href); await page.getByRole('link', { name: `查看成交 ${buy}`, exact: true }).waitFor();
    await page.route(`**${api}?*`, route => route.fulfill({ status: 403, contentType: 'application/json', body: '{"code":"FORBIDDEN"}' })); await page.getByRole('button', { name: '刷新成交', exact: true }).click(); await page.getByText('当前账户无权查看成交。', { exact: true }).waitFor(); assert.equal(await rows.count(), 0);
    await page.unroute(`**${api}?*`); await page.getByRole('button', { name: '重试成交读取', exact: true }).click(); await page.getByRole('link', { name: `查看成交 ${buy}`, exact: true }).waitFor();
    stage = 'actual-revoked-cookie'; const cookies = await context.cookies(origin.href); const csrf = cookies.find(cookie => cookie.name === 'muskdeer_csrf'); assert.ok(csrf);
    const logout = await context.request.post(new URL('/api/v1/auth/logout', origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': csrf.value } }); assert.equal(logout.status(), 204); await context.addCookies(cookies);
    await page.getByRole('button', { name: '刷新成交', exact: true }).click(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor();
    assert.equal(await rows.count(), 0); assert.deepEqual(writes, []); assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    await context.close();
  } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Trades browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
