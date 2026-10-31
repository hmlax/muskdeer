import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1'); assert.equal(origin.pathname, '/');
  assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_RISK_PRIMARY: primary, MUSKDEER_BROWSER_RISK_OTHER: other, MUSKDEER_BROWSER_RISK_FIRST: first, MUSKDEER_BROWSER_RISK_NEXT: next, MUSKDEER_BROWSER_RISK_STRATEGY: strategy, MUSKDEER_BROWSER_RISK_ACCOUNT: account, MUSKDEER_BROWSER_RISK_INSTRUMENT: instrument } = process.env;
  assert.ok(password && artifacts && /^[A-Za-z0-9_.:-]{1,64}$/.test(account ?? '') && [primary, other, first, next, strategy, instrument].every(value => /^[1-9][0-9]*$/.test(value ?? '')));
  await mkdir(artifacts, { recursive: true, mode: 0o700 }); stage = 'browser-launch'; const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    const page = await context.newPage(); page.setDefaultTimeout(8000); const errors = []; const writes = []; const reads = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { const path = new URL(request.url()).pathname; if (request.method() !== 'GET' && !path.startsWith('/api/v1/auth/')) writes.push(path); if (path.startsWith('/api/v1/risk-events')) reads.push(path); });
    const detail = page.getByRole('region', { name: '风险历史详情', exact: true });
    const input = page.getByRole('region', { name: '记录时的风险输入', exact: true });
    const market = page.getByRole('region', { name: '记录时的行情引用', exact: true });
    const rows = page.getByRole('region', { name: '风险历史列表', exact: true }).locator('tbody tr');
    async function field(region, name, value) { const cell = region.getByText(name, { exact: true }).locator('..').locator('dd'); const exact = new RegExp('^' + value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '$'); await cell.filter({ hasText: exact }).waitFor(); assert.equal(await cell.textContent(), value); }
    async function apply({ strategyID = '', accountID = '', instrumentID = '', source = '', kind = '', reason = '', limit = '20' } = {}) {
      await page.getByLabel('策略 ID', { exact: true }).fill(strategyID); await page.getByLabel('账户 ID', { exact: true }).fill(accountID); await page.getByLabel('交易对 ID', { exact: true }).fill(instrumentID);
      await page.getByLabel('记录行情来源', { exact: true }).selectOption(source); await page.getByLabel('记录类型', { exact: true }).selectOption(kind); await page.getByLabel('记录原因', { exact: true }).selectOption(reason); await page.getByLabel('每页事件', { exact: true }).selectOption(limit); await page.getByRole('button', { name: '筛选风险历史', exact: true }).click();
    }
    stage = 'actual-viewer-deep-login'; await page.goto(new URL(`/risk-events/${other}`, origin).href);
    await page.getByLabel('用户名', { exact: true }).fill('risk-events-viewer'); await page.getByLabel('密码', { exact: true }).fill(password); await page.getByRole('button', { name: '登录', exact: true }).click();
    await field(detail, '事件 ID', other); await field(detail, '源退役或移除', 'true'); await field(market, '行情绑定 ID', '未记录'); await field(market, '行情序列', '9007199254740993');
    assert.equal(new URL(page.url()).pathname, `/risk-events/${other}`); await page.reload(); await field(detail, '事件 ID', other);
    stage = 'actual-list-and-cursor'; await page.getByRole('navigation', { name: '控制台导航', exact: true }).getByRole('link', { name: '风险历史', exact: true }).click(); await page.getByRole('link', { name: `查看风险事件 ${other}`, exact: true }).waitFor(); assert.equal(await rows.count(), 6);
    await page.screenshot({ path: `${artifacts}/risk-events-desktop.png`, fullPage: true });
    await apply({ limit: '1' }); await rows.nth(1).waitFor({ state: 'detached' }); await page.getByRole('link', { name: `查看风险事件 ${first}`, exact: true }).waitFor(); assert.equal(await rows.count(), 1);
    await page.getByRole('button', { name: '下一页事件', exact: true }).click(); await page.getByRole('link', { name: `查看风险事件 ${next}`, exact: true }).waitFor();
    await page.getByRole('button', { name: '上一页事件', exact: true }).click(); await page.getByRole('link', { name: `查看风险事件 ${first}`, exact: true }).waitFor();
    stage = 'actual-combined-filter-and-refresh'; await apply({ strategyID: strategy, accountID: account, instrumentID: instrument, source: 'binance', kind: 'REMOVED', reason: 'READY' });
    await page.getByRole('link', { name: `查看风险事件 ${primary}`, exact: true }).waitFor(); assert.equal(await rows.count(), 1); assert.equal(await page.getByRole('button', { name: '下一页事件', exact: true }).isDisabled(), true);
    await page.reload(); await page.getByRole('link', { name: `查看风险事件 ${primary}`, exact: true }).waitFor(); assert.equal(await page.getByLabel('记录类型', { exact: true }).inputValue(), 'REMOVED'); assert.equal(await page.getByLabel('记录原因', { exact: true }).inputValue(), 'READY');
    stage = 'actual-empty-source-isolation-and-local-validation'; await apply({ strategyID: strategy, source: 'okx' }); await page.getByText('没有匹配的风险历史记录', { exact: true }).waitFor();
    const unknownRead = page.waitForResponse(response => { const url = new URL(response.url()); return url.pathname === '/api/v1/risk-events' && url.searchParams.get('account_id') === 'unknown-risk-account'; });
    await apply({ accountID: 'unknown-risk-account' }); await unknownRead; await page.getByText('没有匹配的风险历史记录', { exact: true }).waitFor();
    const beforeInvalid = reads.length; await apply({ strategyID: '01' }); await page.getByText('请输入规范账户 ID 和正整数策略、交易对 ID，或留空查询全部。', { exact: true }).waitFor(); assert.equal(reads.length, beforeInvalid);
    await page.goto(new URL('/risk-events/9223372036854775807', origin).href); await page.getByText('未找到此风险历史记录。', { exact: true }).waitFor(); assert.equal(await detail.count(), 0);
    stage = 'actual-strategy-link-and-removed-source'; await page.goto(new URL(`/strategies/${strategy}`, origin).href); await page.getByRole('link', { name: '查看风险历史 ↗', exact: true }).click(); await page.getByRole('link', { name: `查看风险事件 ${primary}`, exact: true }).waitFor(); assert.equal(await rows.count(), 4);
    await page.getByRole('link', { name: `查看风险事件 ${primary}`, exact: true }).click(); await field(detail, '记录类型', 'REMOVED'); await field(detail, '源退役或移除', 'true'); await field(input, '记录原因', 'READY'); await field(input, '记录许可位', 'true'); await field(input, '记录对账位', 'false');
    await input.getByText('仅展示当时保存的诊断。许可位、READY 和记录期限不代表当前风险许可；读取不会补账或恢复交易。', { exact: true }).waitFor();
    const originalRevision = await detail.getByText('源快照修订', { exact: true }).locator('..').locator('dd').textContent(); assert.match(originalRevision, /^[1-9][0-9]*$/);
    stage = 'actual-tablet'; await page.setViewportSize({ width: 800, height: 1000 }); await page.screenshot({ path: `${artifacts}/risk-events-tablet.png`, fullPage: true }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
    stage = 'actual-mobile'; await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/risk-events-mobile.png`, fullPage: true });
    stage = 'protocol-high-integers-and-old-times'; await page.route(`**/api/v1/risk-events/${primary}`, async route => {
      const response = await route.fetch(); const body = await response.json(); body.event.snapshot_revision = '9007199254740993'; body.event.config_version = '9007199254740994'; body.event.market_feed_id = '9007199254740995'; body.event.market_sequence = '9223372036854775807'; body.event.recorded_at_ms = '9223372036854775807'; body.event.observed_at_ms = '1000'; body.event.expires_at_ms = '2000'; await route.fulfill({ response, json: body });
    });
    await page.getByRole('button', { name: '刷新风险历史', exact: true }).click(); await field(detail, '源快照修订', '9007199254740993'); await field(detail, '记录配置版本', '9007199254740994'); await field(detail, '记录时间（UTC）', '9223372036854775807'); await field(market, '行情序列', '9223372036854775807'); await field(market, '行情绑定 ID', '9007199254740995'); await field(input, '记录许可位', 'true'); await field(input, '记录对账位', 'false');
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/risk-events-precision-mobile.png`, fullPage: true });
    await page.unroute(`**/api/v1/risk-events/${primary}`); await page.getByRole('button', { name: '刷新风险历史', exact: true }).click(); await field(detail, '源快照修订', originalRevision);
    for (const [name, change] of [
      ['numeric', value => { value.market_sequence = 1; }],
      ['markup', value => { value.reason_code = '<img src=x onerror="window.__riskEventProbe=1">'; }],
    ]) {
      stage = `protocol-${name}-rejected`; await page.route(`**/api/v1/risk-events/${primary}`, async route => { const response = await route.fetch(); const body = await response.json(); change(body.event); await route.fulfill({ response, json: body }); });
      await page.getByRole('button', { name: '刷新风险历史', exact: true }).click(); await page.getByText('暂时无法读取风险历史，请稍后重试。', { exact: true }).waitFor(); assert.equal(await detail.count(), 0); assert.equal(await page.locator('img').count(), 0); assert.equal(await page.evaluate(() => window.__riskEventProbe), undefined);
      await page.unroute(`**/api/v1/risk-events/${primary}`); await page.getByRole('button', { name: '重试风险历史读取', exact: true }).click(); await field(detail, '事件 ID', primary);
    }
    stage = 'permission-failure-clears-old-list'; await page.goto(new URL('/risk-events', origin).href); await page.getByRole('link', { name: `查看风险事件 ${other}`, exact: true }).waitFor();
    await page.route('**/api/v1/risk-events?*', route => route.fulfill({ status: 403, contentType: 'application/json', body: '{"code":"FORBIDDEN"}' })); await page.getByRole('button', { name: '刷新风险历史', exact: true }).click(); await page.getByText('当前账户无权查看风险历史。', { exact: true }).waitFor(); assert.equal(await rows.count(), 0);
    await page.unroute('**/api/v1/risk-events?*'); await page.getByRole('button', { name: '重试风险历史读取', exact: true }).click(); await page.getByRole('link', { name: `查看风险事件 ${other}`, exact: true }).waitFor();
    stage = 'actual-revoked-cookie'; const cookies = await context.cookies(origin.href); const csrf = cookies.find(cookie => cookie.name === 'muskdeer_csrf'); assert.ok(csrf);
    const logout = await context.request.post(new URL('/api/v1/auth/logout', origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': csrf.value } }); assert.equal(logout.status(), 204); await context.addCookies(cookies);
    await page.getByRole('button', { name: '刷新风险历史', exact: true }).click(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor();
    assert.equal(await rows.count(), 0); assert.equal(await detail.count(), 0); assert.deepEqual(writes, []); assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    await context.close();
  } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Risk history browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
