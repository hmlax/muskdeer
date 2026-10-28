import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_ACCOUNT: account, MUSKDEER_BROWSER_INSTRUMENT: instrument, MUSKDEER_BROWSER_CODE: code } = process.env;
  assert.ok(password && artifacts && account && /^[1-9][0-9]*$/.test(instrument ?? '') && code);
  await mkdir(artifacts, { recursive: true, mode: 0o700 });
  const browser = await chromium.launch({ headless: true });
  let page;
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    page = await context.newPage(); page.setDefaultTimeout(8000);
    const errors = []; const writes = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/strategies') writes.push({ key: request.headers()['idempotency-key'], body: request.postDataJSON() }); });
    async function login(username) {
      await page.goto(new URL('/strategies', origin).href);
      await page.getByLabel('用户名', { exact: true }).fill(username);
      await page.getByLabel('密码', { exact: true }).fill(password);
      await page.getByRole('button', { name: '登录', exact: true }).click();
      await page.getByRole('heading', { name: '策略实例', exact: true }).waitFor();
    }
    stage = 'operator-login'; await login('control-operator');
    let first = true;
    for (const template of ['grid', 'market_maker']) {
      for (const source of ['binance', 'okx']) {
        stage = `create-${template}-${source}`;
        stage = `create-${template}-${source}-open-form`;
        if (first) await page.route('**/api/v1/accounts?*', route => route.abort());
        await page.getByRole('button', { name: '创建策略', exact: true }).click();
        const form = page.getByRole('region', { name: '创建策略', exact: true });
        if (first) {
          stage = 'create-directory-retry';
          await form.getByText('目录读取失败。', { exact: false }).waitFor();
          await page.unroute('**/api/v1/accounts?*');
          await form.getByRole('button', { name: '重试目录', exact: true }).click();
          await form.getByRole('button', { name: '加载更多账户', exact: true }).click();
          const lastAccount = form.getByLabel('账户', { exact: true }).locator('option').filter({ hasText: 'creation-page-account-101' });
          await lastAccount.waitFor({ state: 'attached' }); stage = 'create-directory-account-disabled'; assert.ok(await lastAccount.evaluate(option => option.disabled && option.matches(':disabled')));
          await form.getByRole('button', { name: '加载更多交易对', exact: true }).click();
          const lastInstrument = form.getByLabel('交易对', { exact: true }).locator('option').filter({ hasText: 'PAGE101-USDT' });
          await lastInstrument.waitFor({ state: 'attached' }); stage = 'create-directory-instrument-disabled'; assert.ok(await lastInstrument.evaluate(option => option.disabled && option.matches(':disabled')));
        }
        const name = `browser-create-${template}-${source}`;
        stage = `create-${template}-${source}-name`;
        await form.getByLabel('实例标识', { exact: true }).fill(name);
        await form.getByLabel('代码版本', { exact: true }).fill(code);
        stage = `create-${template}-${source}-account`;
        await form.getByLabel('账户', { exact: true }).selectOption(account);
        stage = `create-${template}-${source}-instrument`;
        await form.getByLabel('交易对', { exact: true }).selectOption(instrument);
        stage = `create-${template}-${source}-template`;
        await form.getByLabel('策略模板', { exact: true }).selectOption(template);
        if (first) {
          await form.getByLabel('行情模式', { exact: true }).selectOption('LIVE');
          stage = `create-${template}-${source}-empty-live`;
        await form.getByText('当前模式没有已绑定来源，请检查行情配置。').waitFor();
          assert.equal(await form.getByLabel('行情来源', { exact: true }).inputValue(), '');
          await form.getByLabel('行情模式', { exact: true }).selectOption('REPLAY');
        }
        stage = `create-${template}-${source}-source`;
        await form.getByLabel('行情来源', { exact: true }).selectOption(source);
        if (template === 'grid') await form.getByLabel('下界价格', { exact: true }).fill('98.123456789012345678');
        else { await form.getByLabel('单档名义额（报价资产）', { exact: true }).fill('10.123456789012345678'); await form.getByLabel('单侧预算（报价资产）', { exact: true }).fill('30'); }
        stage = `create-${template}-${source}-parameters-complete`;
        const before = writes.length;
        await form.getByRole('button', { name: '确认创建', exact: true }).click();
        assert.equal(writes.length, before);
        await form.getByLabel('创建原因', { exact: true }).fill(`browser creates ${template} ${source}`);
        if (first) {
          await page.setViewportSize({ width: 390, height: 844 });
          stage = 'create-mobile-overflow'; assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
          await page.screenshot({ path: `${artifacts}/creation-form-mobile.png`, fullPage: true });
          stage = 'create-submit-response-unknown';
          let dropped = false;
          await page.route('**/api/v1/strategies', async route => {
            if (route.request().method() !== 'POST' || dropped) { await route.continue(); return; }
            const result = await route.fetch(); assert.equal(result.status(), 201); dropped = true; await route.abort();
          });
          await form.getByRole('button', { name: '确认创建', exact: true }).click();
          await form.getByText('尚未确认创建结果，请按原请求重试；离开页面不会撤回请求。').waitFor();
          await page.screenshot({ path: `${artifacts}/creation-unknown-mobile.png`, fullPage: true });
          await page.unroute('**/api/v1/strategies');
          await form.getByRole('button', { name: '按原请求重试创建', exact: true }).click();
        } else await form.getByRole('button', { name: '确认创建', exact: true }).click();
        stage = `create-${template}-${source}-receipt`;
        await form.getByText('策略已创建。尚未请求运行，请进入详情查看当前状态并单独启动。').waitFor();
        const submitted = writes.slice(before);
        assert.equal(submitted.length, first ? 2 : 1);
        assert.ok(submitted.every(w => w.key === submitted[0].key && w.body.instance_key === name && w.body.market_data_source === source && typeof w.body.instrument_id === 'string'));
        const values = submitted[0].body.parameters[template];
        assert.equal(typeof values[template === 'grid' ? 'lower_price' : 'level_notional'], 'string');
        await form.getByRole('link', { name: '查看新策略 ↗', exact: true }).click();
        await page.getByRole('heading', { name, exact: true }).waitFor();
        await page.reload(); await page.getByRole('heading', { name, exact: true }).waitFor();
        await page.getByText(template === 'grid' ? '98.123456789012345678' : '10.123456789012345678', { exact: true }).first().waitFor();
        await page.setViewportSize({ width: 1440, height: 1000 });
        if (template === 'market_maker' && source === 'okx') await page.screenshot({ path: `${artifacts}/creation-maker-detail.png`, fullPage: true });
        await page.goto(new URL('/strategies', origin).href); await page.getByRole('heading', { name: '策略实例', exact: true }).waitFor();
        first = false;
      }
    }
    assert.deepEqual(errors, []);
    assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
    await page.goto(new URL('/session', origin).href);
    await page.getByRole('button', { name: '退出当前会话', exact: true }).click();
    await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor();
    stage = 'viewer-read-only'; await login('creation-viewer');
    assert.equal(await page.getByRole('button', { name: '创建策略', exact: true }).count(), 0);
    await page.getByRole('link', { name: 'browser-create-grid-okx', exact: true }).waitFor();
    await page.screenshot({ path: `${artifacts}/creation-viewer-list.png`, fullPage: true });
    await context.close();
  } catch (error) { if (page && !page.isClosed() && stage === 'create-mobile-overflow') console.log('Creation layout bounds', JSON.stringify(await page.evaluate(() => ({ viewport: innerWidth, width: document.documentElement.scrollWidth, elements: [...document.querySelectorAll('body *')].map(el => ({ tag: el.tagName, cls: String(el.className), right: Math.round(el.getBoundingClientRect().right), width: Math.round(el.getBoundingClientRect().width) })).filter(el => el.right > innerWidth + 1).slice(-12) })))); if (page && !page.isClosed() && stage.startsWith('create-')) await page.screenshot({ path: `${artifacts}/creation-failure.png`, fullPage: true }); throw error; } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Creation browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
