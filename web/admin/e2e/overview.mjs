import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1'); assert.equal(origin.pathname, '/'); assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_OVERVIEW_PHASE: phase, MUSKDEER_BROWSER_OVERVIEW_STRATEGY: strategy, MUSKDEER_BROWSER_OVERVIEW_GRID: grid, MUSKDEER_BROWSER_OVERVIEW_MAKER: maker, MUSKDEER_BROWSER_OVERVIEW_ACCOUNT: account } = process.env;
  assert.ok(password && artifacts && ['healthy', 'removed'].includes(phase) && /^[A-Za-z0-9_.:-]{1,64}$/.test(account ?? '') && [strategy, grid, maker].every(value => /^[1-9][0-9]*$/.test(value ?? '')));
  await mkdir(artifacts, { recursive: true, mode: 0o700 }); stage = 'browser-launch'; const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    const page = await context.newPage(); page.setDefaultTimeout(8000); const errors = []; const writes = []; const reads = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { const path = new URL(request.url()).pathname; if (request.method() !== 'GET' && !path.startsWith('/api/v1/auth/')) writes.push(path); if (path.startsWith('/api/v1/overview/')) reads.push(request.url()); });
    const strategies = page.getByRole('region', { name: '策略运行观测', exact: true }); const services = page.getByRole('region', { name: '服务运行观测', exact: true });
    const strategyRows = strategies.locator('article'); const nodeRows = services.locator('article');
    const row = id => page.getByRole('article', { name: `策略运行观测 ${id}`, exact: true });
    const node = id => page.getByRole('article', { name: `服务节点 ${id}`, exact: true });
    const escaped = value => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    async function exact(locator, value) { await locator.filter({ hasText: new RegExp('^' + escaped(value) + '$') }).waitFor(); assert.equal(await locator.textContent(), value); }
    async function field(region, name, value) { await exact(region.getByText(name, { exact: true }).locator('..').locator('dd'), value); }
    async function strategyTotal(value) { await exact(strategies.locator('[data-overview-total]'), value); }
    async function serviceTotal(value) { await exact(services.locator('[data-service-total]'), value); }
    async function applyStrategies({ accountID = '', source = '', limit = '20' } = {}) {
      await page.getByLabel('概览账户 ID', { exact: true }).fill(accountID); await page.getByLabel('请求行情来源', { exact: true }).selectOption(source); await page.getByLabel('每页策略', { exact: true }).selectOption(limit); await page.getByRole('button', { name: '筛选策略概览', exact: true }).click();
    }
    async function applyServices(role = 'strategy', limit = '4') {
      await page.getByLabel('服务角色', { exact: true }).selectOption(role); await page.getByLabel('每页节点', { exact: true }).selectOption(limit); await page.getByRole('button', { name: '查询服务角色', exact: true }).click();
    }
    stage = 'actual-deep-login'; await page.goto(new URL('/overview?role=api&source=okx', origin).href);
    await page.getByLabel('用户名', { exact: true }).fill('overview-viewer'); await page.getByLabel('密码', { exact: true }).fill(password); await page.getByRole('button', { name: '登录', exact: true }).click();
    await strategyTotal('2'); await serviceTotal('1'); await field(row(grid), '存储状态', 'CREATED'); await row(maker).getByText('无持久租约槽', { exact: true }).waitFor(); await field(row(grid), '租约 epoch', '0'); await field(row(grid), '保留持有者', 'false'); await field(row(grid), '记录期限（UTC）', '0 · 未设置');
    assert.equal(new URL(page.url()).pathname, '/overview'); assert.equal(new URL(page.url()).searchParams.get('source'), 'okx'); await page.reload(); await strategyTotal('2'); await serviceTotal('1');
    await services.getByText('仅表示处理此请求的 API，不枚举其他 API 副本。', { exact: true }).waitFor();
    stage = 'actual-all-strategies-and-retained-lease'; await applyStrategies(); await strategyTotal('3'); await exact(strategies.locator('[data-overview-requested]'), '1'); await field(row(strategy), '存储状态', 'RUNNING'); await field(row(strategy), '请求目标', 'RUNNING');
    await row(strategy).getByRole('heading', { name: '已释放的保留租约', exact: true }).waitFor(); await field(row(strategy), '保留持有者', 'true'); await field(row(strategy), '记录期限（UTC）', '0 · 已清零'); await field(row(strategy), '数据库采样时未到期', 'false'); await field(row(strategy), '与存储运行记录匹配', 'true');
    stage = 'actual-named-service-membership'; await applyServices(); await serviceTotal(phase === 'healthy' ? '2' : '1'); await field(node('overview-browser-a'), '探测结果', 'READY'); await field(node('overview-browser-a'), '声明版本', 'declared-example-a');
    if (phase === 'removed') {
      assert.equal(await node('overview-browser-b').count(), 0); assert.equal(await strategyRows.count(), 3); await page.screenshot({ path: `${artifacts}/overview-removed.png`, fullPage: true });
    } else {
      await field(node('overview-browser-b'), '声明版本', 'declared-example-b'); await field(node('overview-browser-b'), '探测结果', 'READY');
      await page.screenshot({ path: `${artifacts}/overview-desktop.png`, fullPage: true });
      stage = 'actual-independent-pagination'; await applyStrategies({ limit: '1' }); await row(grid).waitFor({ state: 'detached' }); await field(row(strategy), '策略 ID', strategy); await strategyTotal('3');
      await page.getByRole('button', { name: '下一页策略观测', exact: true }).click(); await field(row(grid), '策略 ID', grid); await strategyTotal('3'); await exact(strategies.locator('[data-overview-requested]'), '1');
      await page.getByRole('button', { name: '上一页策略观测', exact: true }).click(); await field(row(strategy), '策略 ID', strategy);
      await applyServices('strategy', '1'); await node('overview-browser-b').waitFor({ state: 'detached' }); await field(node('overview-browser-a'), '节点 ID', 'overview-browser-a'); await serviceTotal('2');
      await page.getByRole('button', { name: '下一页节点', exact: true }).click(); await field(node('overview-browser-b'), '节点 ID', 'overview-browser-b'); await serviceTotal('2'); await page.reload(); await field(node('overview-browser-b'), '节点 ID', 'overview-browser-b'); await field(row(strategy), '策略 ID', strategy);
      stage = 'actual-empty-and-unconfigured-roles'; await applyServices('market'); await serviceTotal('1'); await field(node('overview-browser-market'), '探测结果', 'PROBE_NOT_CONFIGURED'); await field(node('overview-browser-market'), '探测时间（UTC）', '未探测');
      await applyServices('cron'); await serviceTotal('0'); await services.getByText('当前页没有符合条件的服务节点', { exact: true }).waitFor();
      stage = 'actual-filters-and-local-validation'; await applyStrategies({ accountID: account, source: 'okx' }); await strategyTotal('2'); await exact(strategies.locator('[data-overview-requested]'), '0'); await row(strategy).waitFor({ state: 'detached' });
      await page.reload(); await strategyTotal('2'); assert.equal(await page.getByLabel('请求行情来源', { exact: true }).inputValue(), 'okx'); assert.equal(await page.getByLabel('概览账户 ID', { exact: true }).inputValue(), account);
      await applyStrategies({ accountID: 'unknown-overview-account' }); await strategyTotal('0'); await strategies.getByText('当前页没有匹配的策略', { exact: true }).waitFor();
      const beforeInvalid = reads.length; await applyStrategies({ accountID: 'bad account' }); await strategies.getByText('请输入规范账户 ID，或留空查询全部。', { exact: true }).waitFor(); assert.equal(reads.length, beforeInvalid);
      await applyStrategies(); await strategyTotal('3'); await applyServices(); await serviceTotal('2');
      stage = 'actual-strategy-link'; await page.getByRole('link', { name: `查看概览策略 ${strategy}`, exact: true }).click(); assert.equal(new URL(page.url()).pathname, `/strategies/${strategy}`); await page.getByRole('navigation', { name: '控制台导航', exact: true }).getByRole('link', { name: '运行概览', exact: true }).click(); await strategyTotal('3'); await serviceTotal('2');
      stage = 'actual-responsive-layout'; await page.setViewportSize({ width: 800, height: 1000 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/overview-tablet.png`, fullPage: true });
      await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/overview-mobile.png`, fullPage: true });
      stage = 'protocol-exact-counters-epochs-and-time'; await applyStrategies({ limit: '1' }); await field(row(strategy), '策略 ID', strategy);
      await page.route('**/api/v1/overview/strategies?*', async route => {
        const response = await route.fetch(); const body = await response.json(); body.total = '9007199254740993'; body.stored_states = [{ state: 'CREATED', count: '9007199254740992' }, { state: 'RUNNING', count: '1' }]; body.observed_at_ms = '9223372036854775807'; body.items[0].strategy.runtime_owner_epoch = '9007199254740994'; body.items[0].lease.epoch = '9007199254740994'; await route.fulfill({ response, json: body });
      });
      await page.getByRole('button', { name: '刷新策略概览', exact: true }).click(); await strategyTotal('9007199254740993'); await field(row(strategy), '租约 epoch', '9007199254740994'); await field(row(strategy), '存储运行 epoch', '9007199254740994'); await strategies.getByText('9223372036854775807', { exact: true }).waitFor(); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/overview-precision-mobile.png`, fullPage: true });
      await page.unroute('**/api/v1/overview/strategies?*'); await page.getByRole('button', { name: '刷新策略概览', exact: true }).click(); await strategyTotal('3');
      for (const [name, mutate] of [
        ['numeric-counter', value => { value.total = 3; }],
        ['inconsistent-lease', value => { value.items[0].lease.unexpired_at_observation = true; }],
        ['wrong-scope', value => { value.items[0].strategy.account_id = 'different-account'; }],
      ]) {
        stage = `protocol-${name}`;
        if (name === 'wrong-scope') { await applyStrategies({ accountID: account, limit: '1' }); await strategyTotal('3'); }
        await page.route('**/api/v1/overview/strategies?*', async route => { const response = await route.fetch(); const body = await response.json(); mutate(body); await route.fulfill({ response, json: body }); });
        await page.getByRole('button', { name: '刷新策略概览', exact: true }).click(); await strategies.getByText('暂时无法读取策略概览，请稍后重试。', { exact: true }).waitFor(); assert.equal(await strategyRows.count(), 0); assert.equal(await strategies.locator('[data-overview-total]').count(), 0);
        await page.unroute('**/api/v1/overview/strategies?*'); await page.getByRole('button', { name: '重试策略概览', exact: true }).click(); await strategyTotal('3');
      }
      for (const [name, mutate] of [
        ['numeric-time', value => { value.membership_observed_at_ms = 1; }],
        ['markup-status', value => { value.items[0].status = '<img src=x onerror="window.__overviewProbe=1">'; }],
      ]) {
        stage = `protocol-${name}`; await page.route('**/api/v1/overview/services?*', async route => { const response = await route.fetch(); const body = await response.json(); mutate(body); await route.fulfill({ response, json: body }); });
        await page.getByRole('button', { name: '刷新服务观测', exact: true }).click(); await services.getByText('暂时无法读取服务观测，请稍后重试。', { exact: true }).waitFor(); assert.equal(await nodeRows.count(), 0); assert.equal(await page.locator('img').count(), 0); assert.equal(await page.evaluate(() => window.__overviewProbe), undefined);
        await page.unroute('**/api/v1/overview/services?*'); await page.getByRole('button', { name: '重试服务观测', exact: true }).click(); await serviceTotal('2');
      }
      stage = 'permission-clears-service-data'; await page.route('**/api/v1/overview/services?*', route => route.fulfill({ status: 403, contentType: 'application/json', body: '{"code":"FORBIDDEN"}' })); await page.getByRole('button', { name: '刷新服务观测', exact: true }).click(); await services.getByText('当前账户无权查看服务观测。', { exact: true }).waitFor(); assert.equal(await nodeRows.count(), 0); assert.equal(await strategyRows.count(), 1);
      await page.unroute('**/api/v1/overview/services?*'); await page.getByRole('button', { name: '重试服务观测', exact: true }).click(); await serviceTotal('2');
    }
    stage = 'actual-logout-old-cookie-denied'; const saved = await context.cookies(); const csrf = saved.find(cookie => cookie.name === 'muskdeer_csrf'); assert.ok(csrf);
    const response = await context.request.post(new URL('/api/v1/auth/logout', origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': csrf.value } }); assert.equal(response.status(), 204); await context.addCookies(saved);
    await page.getByRole('button', { name: '刷新服务观测', exact: true }).click(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor(); assert.equal(await strategyRows.count(), 0); assert.equal(await nodeRows.count(), 0);
    assert.deepEqual(errors, []); assert.deepEqual(writes, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0); await context.close();
  } finally { await browser.close(); }
}
run().catch(error => { console.error(`Overview browser failed at ${stage}: ${error.name}: ${error.message}`); process.exitCode = 1; });
