import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1'); assert.equal(origin.pathname, '/');
  assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_PORTFOLIO_PRIMARY: primary, MUSKDEER_BROWSER_PORTFOLIO_OTHER: other, MUSKDEER_BROWSER_PORTFOLIO_ACCOUNT: account, MUSKDEER_BROWSER_PORTFOLIO_INSTRUMENT: instrument, MUSKDEER_BROWSER_PORTFOLIO_STRATEGY: strategy } = process.env;
  assert.ok(password && artifacts && /^[A-Za-z0-9_.:-]{1,64}$/.test(account ?? '') && [primary, other, instrument, strategy].every(value => /^[1-9][0-9]*$/.test(value ?? '')));
  await mkdir(artifacts, { recursive: true, mode: 0o700 }); stage = 'browser-launch'; const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    const page = await context.newPage(); page.setDefaultTimeout(8000); const errors = []; const writes = []; const reads = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { const path = new URL(request.url()).pathname; if (request.method() !== 'GET' && !path.startsWith('/api/v1/auth/')) writes.push(path); if (path.startsWith('/api/v1/portfolios')) reads.push(path); });
    const detail = page.getByRole('region', { name: '账户持仓详情', exact: true });
    const progress = page.getByRole('region', { name: '投影诊断与成交进度', exact: true });
    const quote = page.getByRole('region', { name: 'USDT 核心余额', exact: true });
    const rows = page.getByRole('region', { name: '账户持仓列表', exact: true }).locator('tbody tr');
    async function field(region, name, value) { const cell = region.getByText(name, { exact: true }).locator('..').locator('dd'); await cell.filter({ hasText: value }).waitFor(); assert.equal(await cell.textContent(), value); }
    async function apply(accountID, instrumentID, limit = '20') { await page.getByLabel('账户 ID', { exact: true }).fill(accountID); await page.getByLabel('交易对 ID', { exact: true }).fill(instrumentID); await page.getByLabel('每页持仓', { exact: true }).selectOption(limit); await page.getByRole('button', { name: '筛选持仓', exact: true }).click(); }
    stage = 'actual-viewer-deep-login'; await page.goto(new URL(`/portfolios/${other}`, origin).href);
    await page.getByLabel('用户名', { exact: true }).fill('portfolios-viewer'); await page.getByLabel('密码', { exact: true }).fill(password); await page.getByRole('button', { name: '登录', exact: true }).click();
    await field(detail, '持仓记录 ID', other); await field(detail, '账户配置', '停用'); await field(detail, '已实现损益（USDT）', '-0.123456789012345678901234567890123456');
    await page.getByText('尚无 BTC 核心余额记录；不能视为零资金。', { exact: true }).waitFor(); await quote.getByText('尚无 USDT 核心余额记录；不能视为零资金。', { exact: true }).waitFor();
    assert.equal(new URL(page.url()).pathname, `/portfolios/${other}`); await page.reload(); await field(detail, '持仓记录 ID', other);
    stage = 'actual-account-list'; await page.getByRole('navigation', { name: '控制台导航', exact: true }).getByRole('link', { name: '账户持仓', exact: true }).click(); await page.getByRole('link', { name: `查看持仓 ${other}`, exact: true }).waitFor(); assert.equal(await rows.count(), 2);
    await page.screenshot({ path: `${artifacts}/portfolios-desktop.png`, fullPage: true });
    stage = 'actual-cursor'; await apply('', '', '1'); await rows.nth(1).waitFor({ state: 'detached' }); await page.getByRole('link', { name: `查看持仓 ${primary}`, exact: true }).waitFor(); assert.equal(await rows.count(), 1);
    await page.getByRole('button', { name: '下一页持仓', exact: true }).click(); await page.getByRole('link', { name: `查看持仓 ${other}`, exact: true }).waitFor(); assert.equal(await page.getByRole('button', { name: '下一页持仓', exact: true }).isDisabled(), true);
    await page.getByRole('button', { name: '上一页持仓', exact: true }).click(); await page.getByRole('link', { name: `查看持仓 ${primary}`, exact: true }).waitFor();
    stage = 'actual-filter-empty-and-validation'; await apply('portfolio-uninitialized', instrument); await page.getByText('没有匹配的账户持仓', { exact: true }).waitFor(); await page.reload(); await page.getByText('没有匹配的账户持仓', { exact: true }).waitFor(); assert.equal(await page.getByLabel('账户 ID', { exact: true }).inputValue(), 'portfolio-uninitialized');
    const beforeInvalid = reads.length; await apply(account, '01'); await page.getByText('请输入规范账户 ID 和正整数交易对 ID，或留空查询全部。', { exact: true }).waitFor(); assert.equal(reads.length, beforeInvalid);
    await apply(account, '9223372036854775807'); await page.getByText('未找到此账户、交易对或持仓。', { exact: true }).waitFor(); assert.equal(await rows.count(), 0);
    stage = 'actual-shared-strategy-link'; await page.goto(new URL(`/strategies/${strategy}`, origin).href); await page.getByRole('link', { name: '查看账户持仓 ↗', exact: true }).click(); await page.getByRole('link', { name: `查看持仓 ${primary}`, exact: true }).waitFor(); assert.equal(await rows.count(), 1); assert.equal(await page.getByLabel('账户 ID', { exact: true }).inputValue(), account);
    await page.getByRole('link', { name: `查看持仓 ${primary}`, exact: true }).click(); await field(detail, '现金投影（USDT）', '1000'); await field(quote, '可用余额（USDT）', '1001.8'); await field(quote, '冻结余额（USDT）', '0');
    await field(progress, '上次投影诊断', '未通过对账'); await field(progress, '存储诊断代码', 'SOURCE_INCOMPLETE'); await field(progress, '当前核心成交数', '2'); await field(progress, '当前收录事实数', '1');
    stage = 'actual-mobile'; await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/portfolios-mobile.png`, fullPage: true });
    stage = 'protocol-exact-loss-and-stored-diagnostic'; const probe = '<img src=x onerror="window.__portfolioProbe=1">'; const large = '1234567890123456789012345678901234567890.123456789012345678901234567890123456';
    await page.route(`**/api/v1/portfolios/${primary}`, async route => { const response = await route.fetch(); const body = await response.json(); body.portfolio.symbol = probe; body.portfolio.account_revision = '9007199254740993'; body.portfolio.projection.realized_pnl_quote = '-' + large; body.portfolio.projection.stored_reconciled = true; body.portfolio.projection.error_code = ''; body.portfolio.core_quote_balance.available = large; body.portfolio.source.latest_source_sequence = '9007199254740994'; await route.fulfill({ response, json: body }); });
    await page.getByRole('button', { name: '刷新持仓', exact: true }).click(); await field(detail, '已实现损益（USDT）', '-' + large); await field(detail, '账户修订', '9007199254740993'); await field(detail, '交易对', probe); await field(quote, '可用余额（USDT）', large); await field(progress, '上次投影诊断', '曾通过对账'); await field(progress, '当前核心最大源 ID', '9007199254740994'); await field(progress, '当前收录事实数', '1');
    await progress.getByText('存储诊断可能早于当前核心事实；计数相同也不能证明已对账。读取不补账、不释放预留，也不开放风险。', { exact: true }).waitFor(); assert.equal(await page.evaluate(() => window.__portfolioProbe), undefined); assert.equal(await detail.locator('img').count(), 0); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/portfolios-precision-mobile.png`, fullPage: true });
    await page.unroute(`**/api/v1/portfolios/${primary}`); await page.getByRole('button', { name: '刷新持仓', exact: true }).click(); await field(detail, '现金投影（USDT）', '1000'); await field(progress, '上次投影诊断', '未通过对账');
    stage = 'protocol-failure-clears-old-money'; await page.goto(new URL('/portfolios', origin).href); await page.getByRole('link', { name: `查看持仓 ${primary}`, exact: true }).waitFor();
    await page.route('**/api/v1/portfolios?*', async route => { const response = await route.fetch(); const body = await response.json(); body.portfolios[0].projection.cash_quote = 1000; await route.fulfill({ response, json: body }); }); await page.getByRole('button', { name: '刷新持仓', exact: true }).click(); await page.getByText('暂时无法读取持仓，请稍后重试。', { exact: true }).waitFor(); assert.equal(await rows.count(), 0);
    await page.unroute('**/api/v1/portfolios?*'); await page.getByRole('button', { name: '重试持仓读取', exact: true }).click(); await page.getByRole('link', { name: `查看持仓 ${primary}`, exact: true }).waitFor();
    stage = 'permission-failure-presentation'; await page.route('**/api/v1/portfolios?*', route => route.fulfill({ status: 403, contentType: 'application/json', body: '{"code":"FORBIDDEN"}' })); await page.getByRole('button', { name: '刷新持仓', exact: true }).click(); await page.getByText('当前账户无权查看持仓。', { exact: true }).waitFor(); assert.equal(await rows.count(), 0);
    await page.unroute('**/api/v1/portfolios?*'); await page.getByRole('button', { name: '重试持仓读取', exact: true }).click(); await page.getByRole('link', { name: `查看持仓 ${primary}`, exact: true }).waitFor();
    stage = 'actual-revoked-cookie'; const cookies = await context.cookies(origin.href); const csrf = cookies.find(cookie => cookie.name === 'muskdeer_csrf'); assert.ok(csrf);
    const logout = await context.request.post(new URL('/api/v1/auth/logout', origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': csrf.value } }); assert.equal(logout.status(), 204); await context.addCookies(cookies);
    await page.getByRole('button', { name: '刷新持仓', exact: true }).click(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor();
    assert.equal(await rows.count(), 0); assert.deepEqual(writes, []); assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    await context.close();
  } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Portfolios browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
