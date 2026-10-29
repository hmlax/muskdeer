import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1');
  assert.equal(origin.pathname, '/'); assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const password = process.env.MUSKDEER_BROWSER_PASSWORD; const artifacts = process.env.MUSKDEER_BROWSER_ARTIFACTS;
  assert.ok(password && artifacts);
  const cases = ['grid', 'market_maker'].flatMap(template => ['binance', 'okx'].map(source => ({ template, source, id: process.env[`MUSKDEER_BROWSER_${template.toUpperCase()}_${source.toUpperCase()}_ID`] })));
  assert.ok(cases.every(c => /^[1-9][0-9]*$/.test(c.id ?? '')));
  await mkdir(artifacts, { recursive: true, mode: 0o700 });
  const browser = await chromium.launch({ headless: true }); let page;
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    page = await context.newPage(); page.setDefaultTimeout(8000);
    const errors = []; const writes = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { if (request.method() === 'POST' && /\/api\/v1\/strategies\/[1-9][0-9]*\/configurations$/.test(new URL(request.url()).pathname)) writes.push({ key: request.headers()['idempotency-key'], body: request.postDataJSON() }); });
    async function login(username) {
      await page.goto(new URL('/strategies', origin).href); await page.getByLabel('用户名', { exact: true }).fill(username); await page.getByLabel('密码', { exact: true }).fill(password);
      await page.getByRole('button', { name: '登录', exact: true }).click(); await page.getByRole('heading', { name: '策略实例', exact: true }).waitFor();
    }
    stage = 'operator-login'; await login('control-operator');
    for (const [index, entry] of cases.entries()) {
      stage = `publication-${entry.template}-${entry.source}-open`;
      const detailPath = `/api/v1/strategies/${entry.id}`; const publishPath = `${detailPath}/configurations`; const target = entry.source === 'binance' ? 'okx' : 'binance';
      await page.goto(new URL(`/strategies/${entry.id}`, origin).href);
      await page.getByRole('heading', { name: `created-${entry.template}-${entry.source}`, exact: true }).waitFor();
      if (index === 0) await page.route('**/api/v1/instruments/*/market-feeds?*', route => route.abort());
      await page.getByRole('button', { name: '发布参数与换源', exact: true }).click();
      const form = page.getByRole('region', { name: '发布参数与换源', exact: true });
      if (index === 0) {
        stage = 'publication-feed-retry'; await form.getByText('来源读取失败。', { exact: false }).waitFor();
        await page.unroute('**/api/v1/instruments/*/market-feeds?*'); await form.getByRole('button', { name: '重试来源', exact: true }).click();
      }
      await form.getByLabel('新版本行情来源', { exact: true }).locator(`option[value="${target}"]`).waitFor({ state: 'attached' });
      assert.equal(await form.getByLabel('新版本行情来源', { exact: true }).inputValue(), entry.source);
      const field = entry.template === 'grid' ? '下界价格' : '单档名义额（报价资产）';
      const original = entry.template === 'grid' ? '100.000000000000000001' : '10.000000000000000001';
      assert.equal(await form.getByLabel(field, { exact: true }).inputValue(), original);
      const precise = entry.template === 'grid' ? '98.123456789012345678' : '10.123456789012345678';
      await form.getByLabel(field, { exact: true }).fill(precise);
      await form.getByLabel(entry.template === 'grid' ? '总预算（报价资产）' : '单侧预算（报价资产）', { exact: true }).fill('35.000000000000000003');
      await form.getByLabel(entry.template === 'grid' ? '库存上限（基础资产）' : '库存硬限（基础资产）', { exact: true }).fill('20');
      await form.getByLabel('新版本行情来源', { exact: true }).selectOption(target);
      const before = writes.length;
      if (index === 0) {
        stage = 'publication-empty-reason'; await form.getByRole('button', { name: '确认发布新版本', exact: true }).click(); assert.equal(writes.length, before);
        await form.getByLabel('发布原因', { exact: true }).fill('browser publication with exact parameters');
        stage = 'publication-draft-survives-refresh'; await page.getByRole('button', { name: '刷新', exact: true }).click();
        await page.getByRole('heading', { name: 'created-grid-binance', exact: true }).waitFor(); assert.equal(await form.getByLabel(field, { exact: true }).inputValue(), precise);
        const actual = await context.request.get(new URL(detailPath, origin).href); assert.equal(actual.status(), 200); const detail = await actual.json();
        const cookie = (await context.cookies(origin.href)).find(c => c.name === 'muskdeer_csrf'); assert.ok(cookie);
        const external = await context.request.post(new URL(publishPath, origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': cookie.value, 'Idempotency-Key': 'browser-pub-external-update-01' }, data: { expected_revision: detail.strategy.revision, expected_config_version: detail.strategy.requested_config_version, market_data_source: entry.source, parameters: detail.requested_config.parameters, reason: 'concurrent actual publication' } });
        assert.equal(external.status(), 201);
        stage = 'publication-actual-conflict'; await form.getByRole('button', { name: '确认发布新版本', exact: true }).click();
        await form.getByText('配置或修订已变化，请刷新并重新载入当前请求后确认。').waitFor();
        await form.getByText('实例已更新，编辑基线需要重新载入。').waitFor();
        let releaseFeed;
        const feedGate = new Promise(resolve => { releaseFeed = resolve; });
        const feedPattern = '**/api/v1/instruments/*/market-feeds?*';
        await page.route(feedPattern, async route => { await feedGate; await route.continue(); });
        try {
          stage = 'publication-reload-start'; await form.getByRole('button', { name: '载入当前请求', exact: true }).click();
          await form.getByText('正在读取所选模式的来源…', { exact: true }).waitFor();
          assert.ok(await form.getByLabel('新版本行情来源', { exact: true }).isDisabled());
          stage = 'publication-reload-parameters'; assert.equal(await form.getByLabel(field, { exact: true }).inputValue(), original);
        } finally { releaseFeed(); await page.unrouteAll({ behavior: 'wait' }); }
        stage = 'publication-reload-source-ready';
        await form.getByLabel('新版本行情来源', { exact: true }).locator(`option[value="${entry.source}"]`).waitFor({ state: 'attached' });
        assert.equal(await form.getByLabel('新版本行情来源', { exact: true }).inputValue(), entry.source);
        await form.getByLabel(field, { exact: true }).fill(precise);
        await form.getByLabel('总预算（报价资产）', { exact: true }).fill('35.000000000000000003');
        await form.getByLabel('库存上限（基础资产）', { exact: true }).fill('20');
        await form.getByLabel('新版本行情来源', { exact: true }).selectOption(target);
      } else await form.getByLabel('发布原因', { exact: true }).fill('browser publication with exact parameters');
      await form.getByLabel('新版本行情来源', { exact: true }).locator(`option[value="${target}"]`).waitFor({ state: 'attached' });
      const submissionStart = writes.length;
      if (index === 0) {
        stage = 'publication-mobile-form'; await page.setViewportSize({ width: 390, height: 844 });
        assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
        await page.screenshot({ path: `${artifacts}/publication-form-mobile.png`, fullPage: true });
        let lost = false;
        await page.route(`**${publishPath}`, async route => { if (route.request().method() === 'POST' && !lost) { lost = true; const response = await route.fetch(); assert.equal(response.status(), 201); await route.abort(); } else await route.continue(); });
        stage = 'publication-committed-response-loss'; await form.getByRole('button', { name: '确认发布新版本', exact: true }).click();
        stage = 'publication-unknown-message';
        await form.getByText('尚未确认发布结果，请按原请求重试；离开页面不会撤回请求。').waitFor();
        stage = 'publication-unknown-frozen';
        assert.ok(await form.getByLabel('发布原因', { exact: true }).isDisabled());
        stage = 'publication-unknown-refresh-click';
        await page.getByRole('button', { name: '刷新', exact: true }).click();
        stage = 'publication-unknown-refresh-heading';
        await page.getByRole('heading', { name: 'created-grid-binance', exact: true }).waitFor();
        stage = 'publication-unknown-refreshed-fields';
        assert.equal(await form.getByLabel(field, { exact: true }).inputValue(), precise);
        await form.getByText('最近读取：请求 v3 / OKX', { exact: false }).waitFor();
        await page.screenshot({ path: `${artifacts}/publication-unknown-mobile.png`, fullPage: true });
        await page.unroute(`**${publishPath}`);
        await form.getByRole('button', { name: '按原请求重试发布', exact: true }).click();
      } else await form.getByRole('button', { name: '确认发布新版本', exact: true }).click();
      stage = `publication-${entry.template}-${entry.source}-receipt`;
      const expectedVersion = index === 0 ? '3' : '2';
      await form.getByText(`已记录请求版本 v${expectedVersion}。请分别核对请求配置与生效配置，发布收据不代表已完成切换。`).waitFor();
      const submitted = writes.slice(submissionStart); assert.equal(submitted.length, index === 0 ? 2 : 1);
      assert.ok(submitted.every(w => w.key === submitted[0].key && w.body.market_data_source === target && w.body.expected_revision === (index === 0 ? '2' : '1') && typeof w.body.expected_config_version === 'string'));
      assert.ok(submitted.every(w => JSON.stringify(w.body) === JSON.stringify(submitted[0].body)));
      assert.equal(submitted[0].body.parameters[entry.template][entry.template === 'grid' ? 'lower_price' : 'level_notional'], precise);
      await page.setViewportSize({ width: 1440, height: 1000 });
      await form.getByRole('button', { name: '关闭发布', exact: true }).click();
      await page.reload(); await page.getByRole('heading', { name: `created-${entry.template}-${entry.source}`, exact: true }).waitFor();
      await page.getByText(precise, { exact: true }).first().waitFor();
      const requested = page.getByRole('region', { name: '请求配置', exact: true });
      await requested.getByText(target === 'okx' ? 'OKX' : 'Binance', { exact: true }).waitFor();
      await page.getByRole('region', { name: '生效配置', exact: true }).getByText('尚无已生效配置。', { exact: true }).waitFor();
      if (index === cases.length - 1) await page.screenshot({ path: `${artifacts}/publication-maker-detail.png`, fullPage: true });
    }
    stage = 'viewer-read-only'; await page.goto(new URL('/session', origin).href); await page.getByRole('button', { name: '退出当前会话', exact: true }).click();
    await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor(); await login('publication-viewer');
    await page.goto(new URL(`/strategies/${cases[0].id}`, origin).href); await page.getByRole('heading', { name: 'created-grid-binance', exact: true }).waitFor();
    assert.equal(await page.getByRole('button', { name: '发布参数与换源', exact: true }).count(), 0);
    await page.screenshot({ path: `${artifacts}/publication-viewer-detail.png`, fullPage: true });
    assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    await context.close();
  } catch (error) { if (page && !page.isClosed()) await page.screenshot({ path: `${artifacts}/publication-failure.png`, fullPage: true }); throw error; } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Publication browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
