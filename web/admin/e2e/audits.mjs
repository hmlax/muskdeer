import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1'); assert.equal(origin.pathname, '/');
  assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_AUDIT_TEXT_ID: textID, MUSKDEER_BROWSER_AUDIT_HIGH_ID: highID, MUSKDEER_BROWSER_AUDIT_STRATEGY_ID: strategyID, MUSKDEER_BROWSER_AUDIT_TEXT: reason } = process.env;
  assert.ok(password && artifacts && reason && [textID, highID, strategyID].every(id => /^[1-9][0-9]*$/.test(id ?? '')));
  await mkdir(artifacts, { recursive: true, mode: 0o700 });
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    const page = await context.newPage(); page.setDefaultTimeout(8000); const errors = []; const writes = []; const reads = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { const path = new URL(request.url()).pathname; if (request.method() !== 'GET' && !path.startsWith('/api/v1/auth/')) writes.push(path); if (request.method() === 'GET' && path === '/api/v1/audits') reads.push(request.url()); });
    const form = page.getByRole('form', { name: '审计筛选' });
    async function ready() { await page.getByRole('region', { name: '审计列表', exact: true }).waitFor(); }
    async function filters(actor, action, resource) {
      const response = page.waitForResponse(reply => { const url = new URL(reply.url()); return url.pathname === '/api/v1/audits' && reply.status() === 200 && (url.searchParams.get('actor') ?? '') === actor && (url.searchParams.get('action') ?? '') === action && (url.searchParams.get('resource') ?? '') === resource; });
      await form.getByLabel('操作者（精确）', { exact: true }).fill(actor); await form.getByLabel('操作标识（精确）', { exact: true }).fill(action); await form.getByLabel('操作对象（精确）', { exact: true }).fill(resource); await form.getByRole('button', { name: '查询记录', exact: true }).click();
      await response; await page.locator('.audit-workspace[aria-busy="false"]').waitFor();
    }
    stage = 'actual-viewer-login'; await page.goto(new URL('/audits', origin).href);
    await page.getByLabel('用户名', { exact: true }).fill('audit-viewer'); await page.getByLabel('密码', { exact: true }).fill(password);
    await page.route('**/api/v1/audits?*', route => route.abort());
    await page.getByRole('button', { name: '登录', exact: true }).click(); await page.getByRole('heading', { name: '审计记录', exact: true }).waitFor();
    stage = 'actual-read-failure-retry'; await page.getByText('暂时无法读取审计记录，请稍后重试。', { exact: true }).waitFor();
    await page.unrouteAll({ behavior: 'wait' }); await page.getByRole('button', { name: '重试', exact: true }).click(); await ready();
    assert.equal(await page.getByRole('region', { name: '审计列表', exact: true }).locator('tbody tr').count(), 20);
    await page.screenshot({ path: `${artifacts}/audit-list-desktop.png`, fullPage: true });
    stage = 'actual-keyset-pages'; await page.getByRole('button', { name: '下一页', exact: true }).click(); await ready();
    await page.getByRole('link', { name: `查看审计 ${highID}`, exact: true }).waitFor();
    const previousPage = page.waitForResponse(reply => { const url = new URL(reply.url()); return url.pathname === '/api/v1/audits' && reply.status() === 200 && url.searchParams.get('after_id') === '0'; });
    await page.getByRole('button', { name: '上一页', exact: true }).click(); await previousPage; await ready();
    stage = 'invalid-filter-no-request'; await form.getByLabel('操作者（精确）', { exact: true }).fill(' control-operator'); const before = reads.length;
    await form.getByRole('button', { name: '查询记录', exact: true }).click(); await page.getByText('筛选条件不能包含前后空白或控制字符，并须符合字段字节限制。').waitFor(); assert.equal(reads.length, before);
    stage = 'actual-exact-filter'; const resource = `strategy_instance:${strategyID}`; await filters('control-operator', 'strategy.publish', resource); await ready();
    assert.equal(await page.getByRole('region', { name: '审计列表', exact: true }).locator('tbody tr').count(), 1); assert.equal(new URL(page.url()).searchParams.get('resource'), resource);
    await page.reload(); await ready(); assert.equal(await form.getByLabel('操作对象（精确）', { exact: true }).inputValue(), resource);
    stage = 'actual-empty-filter'; await filters('missing-actor', '', ''); await page.getByRole('heading', { name: '没有匹配的审计记录', exact: true }).waitFor();
    await form.getByRole('button', { name: '清空筛选', exact: true }).click(); await ready();
    stage = 'protocol-failure-no-partial-history'; await page.route('**/api/v1/audits?*', async route => { const response = await route.fetch(); const body = await response.json(); body.audits[0].id = 1; await route.fulfill({ response, json: body }); });
    await page.getByRole('button', { name: '刷新', exact: true }).click(); await page.getByText('暂时无法读取审计记录，请稍后重试。', { exact: true }).waitFor(); assert.equal(await page.getByRole('region', { name: '审计列表', exact: true }).count(), 0);
    await page.unrouteAll({ behavior: 'wait' }); await page.getByRole('button', { name: '重试', exact: true }).click(); await ready();
    stage = 'forbidden-state'; await page.route('**/api/v1/audits?*', route => route.fulfill({ status: 403, contentType: 'application/json', json: { code: 'FORBIDDEN' } }));
    await page.getByRole('button', { name: '刷新', exact: true }).click(); await page.getByText('当前账户无权读取审计记录。', { exact: true }).waitFor();
    await page.unrouteAll({ behavior: 'wait' }); await page.getByRole('button', { name: '重试', exact: true }).click(); await ready();
    stage = 'actual-text-detail'; await page.goto(new URL(`/audits/${textID}`, origin).href); await page.getByRole('heading', { name: '审计详情', exact: true }).waitFor();
    const detail = page.getByRole('region', { name: '审计详情', exact: true }); await detail.getByText(reason, { exact: true }).waitFor();
    assert.equal(await page.locator('img[src="x"]').count(), 0); assert.equal(await page.evaluate(() => '__auditExecuted' in window), false);
    assert.equal(await detail.getByText('变更前修订', { exact: true }).locator('..').locator('dd').textContent(), '—');
    await page.setViewportSize({ width: 390, height: 844 }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/audit-detail-mobile.png`, fullPage: true });
    stage = 'actual-high-revisions'; await page.goto(new URL(`/audits/${highID}`, origin).href); await detail.getByText(highID, { exact: true }).waitFor(); await detail.getByText('9007199254740994', { exact: true }).waitFor(); await detail.getByText('9007199254740995', { exact: true }).waitFor();
    await page.reload(); await detail.getByText(highID, { exact: true }).waitFor(); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/audit-precision-mobile.png`, fullPage: true });
    stage = 'actual-not-found'; await page.goto(new URL('/audits/9223372036854775807', origin).href); await page.getByText('未找到此审计记录。', { exact: true }).waitFor();
    stage = 'actual-expired-cookie'; await page.goto(new URL('/audits', origin).href); await ready(); const cookies = await context.cookies(origin.href); const csrf = cookies.find(cookie => cookie.name === 'muskdeer_csrf'); assert.ok(csrf);
    const logout = await context.request.post(new URL('/api/v1/auth/logout', origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': csrf.value } }); assert.equal(logout.status(), 204); await context.addCookies(cookies);
    await page.getByRole('button', { name: '刷新', exact: true }).click(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor(); assert.equal(await page.getByRole('region', { name: '审计列表', exact: true }).count(), 0);
    stage = 'actual-operator-login'; await page.getByLabel('用户名', { exact: true }).fill('control-operator'); await page.getByLabel('密码', { exact: true }).fill(password); await page.getByRole('button', { name: '登录', exact: true }).click();
    await page.getByRole('link', { name: '审计记录', exact: true }).click(); await ready();
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)); await page.screenshot({ path: `${artifacts}/audit-list-mobile.png`, fullPage: true });
    assert.deepEqual(writes, []); assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0); await context.close();
  } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Audit browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
