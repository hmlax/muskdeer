import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1'); assert.equal(origin.pathname, '/');
  assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_CANCEL_STRATEGY: strategy, MUSKDEER_BROWSER_CANCEL_ORDER: order, MUSKDEER_BROWSER_CANCEL_RECEIPT: receipt, MUSKDEER_BROWSER_CANCEL_PHASE: phase, MUSKDEER_BROWSER_CANCEL_REASON: reason } = process.env;
  assert.ok(password && artifacts && reason && [strategy, order].every(value => /^[1-9][0-9]*$/.test(value ?? '')) && ['request', 'pending', 'terminal'].includes(phase));
  if (phase !== 'request') assert.match(receipt, /^[1-9][0-9]*$/);
  await mkdir(artifacts, { recursive: true, mode: 0o700 }); stage = 'browser-launch'; const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    const page = await context.newPage(); page.setDefaultTimeout(8000); const errors = []; const writes = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { if (request.method() === 'POST' && !new URL(request.url()).pathname.startsWith('/api/v1/auth/')) writes.push({ path: new URL(request.url()).pathname, key: request.headers()['idempotency-key'], body: request.postData() }); });
    const submitPath = `/api/v1/strategies/${strategy}/orders/${order}/cancellations`;
    const pagePath = `/strategies/${strategy}/order-cancellations/${receipt}`;
    const resultPath = `/api/v1/strategies/${strategy}/order-cancellations/${receipt}`;
    stage = `${phase}-actual-login`; await page.goto(new URL(phase === 'request' ? `/strategies/${strategy}` : pagePath, origin).href);
    await page.getByLabel('用户名', { exact: true }).fill(phase === 'request' ? 'control-operator' : 'cancellation-viewer'); await page.getByLabel('密码', { exact: true }).fill(password); await page.getByRole('button', { name: '登录', exact: true }).click();
    const record = page.getByLabel('撤单记录', { exact: true });
    async function field(name, value) { const cell = record.getByText(name, { exact: true }).locator('..').locator('dd'); await cell.filter({ hasText: value }).waitFor(); assert.equal(await cell.textContent(), value); }
    if (phase === 'request') {
      const orders = page.getByRole('region', { name: '订单与核心事实', exact: true });
      await orders.getByRole('button', { name: '查看订单', exact: true }).click(); await orders.getByRole('button', { name: `查看订单 ${order}`, exact: true }).click();
      const panel = orders.getByRole('region', { name: '指定订单撤单', exact: true });
      await panel.getByRole('button', { name: '撤销此订单', exact: true }).click(); await panel.getByLabel('撤单原因', { exact: true }).fill(reason);
      assert.equal(await panel.getByRole('button', { name: '确认撤销此订单', exact: true }).isDisabled(), true);
      await panel.getByRole('checkbox', { name: '我确认只撤销此订单，受理不代表订单已终止', exact: true }).check();
      await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
      await panel.screenshot({ path: `${artifacts}/cancellation-confirmation-mobile.png` }); await page.setViewportSize({ width: 1440, height: 1000 });
      let lost = false;
      await page.route(new URL(submitPath, origin).href, async route => {
        if (lost) { await route.continue(); return; }
        lost = true; const reply = await route.fetch(); assert.equal(reply.status(), 202); await route.abort();
      });
      stage = 'actual-committed-response-loss'; await panel.getByRole('button', { name: '确认撤销此订单', exact: true }).click();
      await panel.getByText('尚未确认撤单请求的受理结果，请按原请求重试；离开页面不会撤回请求。', { exact: true }).waitFor();
      assert.equal(await panel.getByLabel('撤单原因', { exact: true }).isDisabled(), true); assert.equal(await orders.getByRole('button', { name: '刷新订单', exact: true }).isDisabled(), true);
      assert.equal(await orders.getByLabel('OMS 状态', { exact: true }).isDisabled(), true); assert.equal(await orders.getByLabel('每页订单', { exact: true }).isDisabled(), true);
      assert.equal(await orders.getByRole('checkbox', { name: '仅开放义务（含 UNKNOWN）', exact: true }).isDisabled(), true);
      assert.equal(await orders.getByRole('button', { name: `查看订单 ${order}`, exact: true }).isDisabled(), true);
      assert.equal(await panel.getByRole('button', { name: '取消撤单确认', exact: true }).count(), 0);
      await panel.getByRole('button', { name: '按原请求重试撤单', exact: true }).click();
      await record.getByText('已受理，待确认', { exact: true }).waitFor(); await field('目标订单 ID', order); await field('提交原因', reason);
      assert.equal(writes.length, 2); assert.deepEqual(writes[0], writes[1]); assert.equal(writes[0].path, submitPath); assert.ok(writes[0].key); assert.equal(JSON.parse(writes[0].body).reason, reason);
      assert.equal(await page.evaluate(() => '__cancelExecuted' in window), false); assert.equal(await page.locator('img[src="x"]').count(), 0);
      await page.screenshot({ path: `${artifacts}/cancellation-request-desktop.png`, fullPage: true });
      await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/cancellation-request-mobile.png`, fullPage: true });
      assert.match(await page.getByRole('link', { name: '打开撤单收据 ↗', exact: true }).getAttribute('href'), new RegExp(`^/strategies/${strategy}/order-cancellations/[1-9][0-9]*$`));
    } else {
      stage = `${phase}-actual-result`; await page.getByRole('heading', { name: '撤单结果', exact: true }).waitFor(); await field('目标订单 ID', order); await field('提交原因', reason);
      assert.equal(await page.getByRole('button', { name: '撤销此订单', exact: true }).count(), 0);
      if (phase === 'pending') {
        await record.getByText('已受理，待确认', { exact: true }).waitFor(); assert.equal(await record.getByText('完成时间', { exact: true }).count(), 0);
        await page.screenshot({ path: `${artifacts}/cancellation-pending.png`, fullPage: true });
      } else {
        await record.getByText('订单已终止', { exact: true }).waitFor(); await field('已确认 OMS 状态', 'CANCELED');
        stage = 'actual-deep-link-refresh'; await page.reload(); await field('已确认 OMS 状态', 'CANCELED');
        await page.screenshot({ path: `${artifacts}/cancellation-terminal-desktop.png`, fullPage: true });
        await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/cancellation-terminal-mobile.png`, fullPage: true });
        stage = 'protocol-number-rejection'; await page.route(new URL(resultPath, origin).href, async route => { const reply = await route.fetch(); const body = await reply.json(); body.expected_order_revision = 1; await route.fulfill({ response: reply, json: body }); });
        await page.getByRole('button', { name: '查询撤单结果', exact: true }).click(); await page.getByText('暂时无法确认撤单结果，请重新查询。', { exact: true }).waitFor(); assert.equal(await record.count(), 0);
        await page.unroute(new URL(resultPath, origin).href); await page.getByRole('button', { name: '查询撤单结果', exact: true }).click(); await field('已确认 OMS 状态', 'CANCELED');
        stage = 'actual-logout-old-cookie'; const cookies = await context.cookies(origin.href); const csrf = cookies.find(cookie => cookie.name === 'muskdeer_csrf'); assert.ok(csrf);
        const logout = await context.request.post(new URL('/api/v1/auth/logout', origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': csrf.value } }); assert.equal(logout.status(), 204); await context.addCookies(cookies);
        await page.getByRole('button', { name: '查询撤单结果', exact: true }).click(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor(); assert.equal(await record.count(), 0);
      }
      assert.deepEqual(writes, []);
    }
    assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0); await context.close();
  } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Cancellation browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
