import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1'); assert.equal(origin.pathname, '/');
  assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_STRATEGY_ID: id, MUSKDEER_BROWSER_STRATEGY_REVISION: revision, MUSKDEER_BROWSER_RISK_PHASE: phase, MUSKDEER_BROWSER_RISK_REASON: reason } = process.env;
  assert.ok(password && artifacts && reason && [id, revision].every(v => /^[1-9][0-9]*$/.test(v ?? '')) && ['block', 'pending', 'restore', 'viewer'].includes(phase));
  await mkdir(artifacts, { recursive: true, mode: 0o700 });
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    const page = await context.newPage(); page.setDefaultTimeout(8000); const errors = []; const writes = [];
    const riskPath = `/api/v1/strategies/${id}/risk`; const riskURL = new URL(riskPath, origin).href;
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { if (request.method() === 'POST' && !new URL(request.url()).pathname.startsWith('/api/v1/auth/')) writes.push({ path: new URL(request.url()).pathname, key: request.headers()['idempotency-key'], body: request.postData() }); });
    const panel = page.getByRole('region', { name: '策略风险与额度', exact: true });
    const current = panel.getByLabel('当前风险快照', { exact: true });
    async function ready(version, reserved, enabled) {
      await page.locator('.risk-card[aria-busy="false"] .risk-current').waitFor();
      await current.getByText('风险管理版本', { exact: true }).locator('..').getByText(version, { exact: true }).waitFor();
      assert.equal(await current.getByText('当前预留（USDT）', { exact: true }).locator('..').locator('dd').textContent(), reserved);
      await current.getByText(enabled ? '资格开关已开启' : '资格开关已关闭', { exact: true }).waitFor();
    }
    async function refresh() {
      const reply = page.waitForResponse(r => r.url() === riskURL && r.request().method() === 'GET' && r.status() === 200);
      await panel.getByRole('button', { name: '刷新风险状态', exact: true }).click(); await reply;
    }
    stage = `${phase}-actual-login`; await page.goto(new URL(`/strategies/${id}`, origin).href);
    await page.getByLabel('用户名', { exact: true }).fill(phase === 'block' ? 'control-operator' : phase === 'viewer' ? 'risk-viewer' : 'risk-administrator');
    await page.getByLabel('密码', { exact: true }).fill(password); await page.getByRole('button', { name: '登录', exact: true }).click();
    await panel.getByRole('button', { name: '查看风险状态', exact: true }).waitFor();
    if (phase === 'block') await page.route(riskURL, route => route.request().method() === 'GET' ? route.abort() : route.continue());
    await panel.getByRole('button', { name: '查看风险状态', exact: true }).click();
    if (phase === 'block') {
      stage = 'actual-read-failure'; await panel.getByText('暂时无法读取风险状态，请重试。', { exact: true }).waitFor(); assert.equal(await current.count(), 0);
      await page.unroute(riskURL); await refresh(); await ready('1', '200', true);
      assert.equal(await panel.getByRole('button', { name: '恢复新风险', exact: true }).count(), 0);
      stage = 'actual-committed-response-loss'; let lost = false;
      await page.route(riskURL, async route => {
        if (route.request().method() !== 'POST' || lost) { await route.continue(); return; }
        lost = true; const response = await route.fetch(); assert.equal(response.status(), 200); await route.abort();
      });
      await panel.getByRole('button', { name: '关闭新风险', exact: true }).click(); await panel.getByLabel('风险操作原因', { exact: true }).fill(reason);
      await panel.getByRole('button', { name: '确认关闭新风险', exact: true }).click();
      await panel.getByText('尚未确认操作结果，请按原请求重试；离开页面不会撤回请求。', { exact: true }).waitFor();
      assert.ok(await panel.getByLabel('风险操作原因', { exact: true }).isDisabled());
      await refresh(); await ready('2', '200', false);
      await panel.getByRole('button', { name: '按原请求重试风险操作', exact: true }).click();
      await panel.getByText('已记录「关闭新风险」，请核对当前风险快照。', { exact: true }).waitFor(); await ready('2', '200', false);
      assert.equal(writes.length, 2); assert.deepEqual(writes[0], writes[1]); assert.ok(writes[0].key); assert.equal(JSON.parse(writes[0].body).reason, reason);
      await page.unroute(riskURL); const receipt = panel.getByLabel('风险操作收据', { exact: true }); await receipt.getByText(reason, { exact: true }).waitFor();
      assert.equal(await page.locator('img[src="x"]').count(), 0); assert.equal(await page.evaluate(() => '__riskExecuted' in window), false);
      await page.screenshot({ path: `${artifacts}/risk-block-desktop.png`, fullPage: true });
      await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/risk-block-mobile.png`, fullPage: true });
      stage = 'actual-logout-old-cookie'; const cookies = await context.cookies(origin.href); const csrf = cookies.find(c => c.name === 'muskdeer_csrf'); assert.ok(csrf);
      const logout = await context.request.post(new URL('/api/v1/auth/logout', origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': csrf.value } }); assert.equal(logout.status(), 204); await context.addCookies(cookies);
      await panel.getByRole('button', { name: '刷新风险状态', exact: true }).click(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor(); assert.equal(await panel.count(), 0);
    } else if (phase === 'pending') {
      await ready('2', '200', false); stage = 'actual-pending-obligation-denial';
      await panel.getByRole('button', { name: '恢复新风险', exact: true }).click(); await panel.getByLabel('风险操作原因', { exact: true }).fill('pending core cancellation cannot restore risk');
      await panel.getByRole('button', { name: '确认恢复新风险', exact: true }).click();
      await panel.getByText('恢复条件未满足。请核对旧订单、预留、对账、额度及所选行情，再读取状态并重新确认。', { exact: true }).waitFor(); await ready('2', '200', false);
      assert.equal(await panel.getByLabel('风险操作收据', { exact: true }).count(), 0); assert.equal(writes.length, 1); await page.screenshot({ path: `${artifacts}/risk-pending-denial.png`, fullPage: true });
    } else if (phase === 'restore') {
      await ready('2', '0', false); stage = 'actual-changed-target-conflict';
      await panel.getByRole('button', { name: '恢复新风险', exact: true }).click(); await panel.getByLabel('风险操作原因', { exact: true }).fill('administrator confirms recovery after exact core terminal');
      const cookies = await context.cookies(origin.href); const csrf = cookies.find(c => c.name === 'muskdeer_csrf'); assert.ok(csrf);
      const pause = await context.request.post(new URL(`/api/v1/strategies/${id}/control`, origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': csrf.value, 'Idempotency-Key': 'risk-browser-explicit-pause-0001' }, data: { action: 'strategy.pause', expected_revision: revision, reason: 'independent target change invalidates old risk confirmation' } }); assert.equal(pause.status(), 202);
      await panel.getByRole('button', { name: '确认恢复新风险', exact: true }).click();
      await panel.getByText('风险版本、配置或运行目标已变化，请重新读取并确认。', { exact: true }).waitFor();
      await current.getByText('请求目标', { exact: true }).locator('..').getByText('已暂停', { exact: true }).waitFor(); await ready('2', '0', false);
      stage = 'actual-explicit-fresh-recovery'; await panel.getByRole('button', { name: '恢复新风险', exact: true }).click();
      await panel.getByRole('form', { name: '确认风险操作', exact: true }).getByText('确认请求配置 v1 · Binance，运行目标「已暂停」。服务端将重新核验旧订单、资金预留、对账、额度及当前所选行情。', { exact: true }).waitFor();
      await panel.getByLabel('风险操作原因', { exact: true }).fill('administrator confirms paused target and fresh selected source');
      await panel.getByRole('button', { name: '确认恢复新风险', exact: true }).click();
      await panel.getByText('已记录「恢复新风险」，请核对当前风险快照。', { exact: true }).waitFor(); await ready('3', '0', true);
      await page.screenshot({ path: `${artifacts}/risk-restored-desktop.png`, fullPage: true });
      assert.equal(writes.filter(w => w.path === riskPath).length, 2);
    } else {
      await ready('3', '0', true); stage = 'actual-viewer-readonly';
      assert.equal(await panel.getByRole('button', { name: '关闭新风险', exact: true }).count(), 0); assert.equal(await panel.getByRole('button', { name: '恢复新风险', exact: true }).count(), 0);
      stage = 'protocol-invalid-no-stale-data'; await page.route(riskURL, async route => { const response = await route.fetch(); const data = await response.json(); data.reserved_notional = 0; await route.fulfill({ response, json: data }); });
      await refresh(); await panel.getByText('暂时无法读取风险状态，请重试。', { exact: true }).waitFor(); assert.equal(await current.count(), 0); await page.unroute(riskURL); await refresh(); await ready('3', '0', true);
      stage = 'protocol-precision-fixture'; const limit = `${'9'.repeat(40)}.${'9'.repeat(36)}`; const reserved = '0.123456789012345678901234567890123456';
      await page.route(riskURL, async route => { const response = await route.fetch(); const data = await response.json(); data.revision = '9007199254740993'; data.requested_config_version = '9007199254740995'; data.limit_notional = limit; data.reserved_notional = reserved; await route.fulfill({ response, json: data }); });
      await refresh(); await ready('9007199254740993', reserved, true); await current.getByText(limit, { exact: true }).waitFor(); await current.getByText('v9007199254740995 · Binance', { exact: true }).waitFor();
      await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/risk-precision-mobile.png`, fullPage: true });
      await page.unroute(riskURL); await refresh(); await ready('3', '0', true);
      stage = 'protocol-forbidden-state'; await page.route(riskURL, route => route.fulfill({ status: 403, contentType: 'application/json', json: { code: 'FORBIDDEN' } }));
      await panel.getByRole('button', { name: '刷新风险状态', exact: true }).click(); await panel.getByText('当前账户无权读取风险状态。', { exact: true }).waitFor(); assert.equal(await current.count(), 0);
      await page.unroute(riskURL); await refresh(); await ready('3', '0', true); assert.deepEqual(writes, []); await page.screenshot({ path: `${artifacts}/risk-viewer-mobile.png`, fullPage: true });
    }
    assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0); await context.close();
  } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Risk browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
