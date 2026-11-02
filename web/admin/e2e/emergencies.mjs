import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:'); assert.equal(origin.hostname, '127.0.0.1'); assert.equal(origin.pathname, '/');
  assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const { MUSKDEER_BROWSER_PASSWORD: password, MUSKDEER_BROWSER_ARTIFACTS: artifacts, MUSKDEER_BROWSER_EMERGENCY_STRATEGY: strategy, MUSKDEER_BROWSER_EMERGENCY_RECEIPT: receipt, MUSKDEER_BROWSER_EMERGENCY_PHASE: phase, MUSKDEER_BROWSER_EMERGENCY_REASON: reason } = process.env;
  assert.ok(password && artifacts && reason && /^[1-9][0-9]*$/.test(strategy ?? '') && ['request', 'pending', 'terminal'].includes(phase));
  if (phase !== 'request') assert.match(receipt, /^[1-9][0-9]*$/);
  await mkdir(artifacts, { recursive: true, mode: 0o700 }); stage = 'launch';
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    const page = await context.newPage(); page.setDefaultTimeout(8000); const errors = []; const writes = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => { if (request.method() === 'POST' && !new URL(request.url()).pathname.startsWith('/api/v1/auth/')) writes.push({ path: new URL(request.url()).pathname, key: request.headers()['idempotency-key'], body: request.postData() }); });
    const historyPath = `/strategies/${strategy}/emergency-cancellations`;
    const detailPath = `${historyPath}/${receipt}`;
    const submitPath = `/api/v1/strategies/${strategy}/emergency-cancellations`;
    const resultPath = `${submitPath}/${receipt}`;
    stage = `${phase}-actual-login`;
    await page.goto(new URL(phase === 'request' ? `/strategies/${strategy}` : detailPath, origin).href);
    await page.getByLabel('用户名', { exact: true }).fill(phase === 'request' ? 'control-operator' : 'emergency-viewer');
    await page.getByLabel('密码', { exact: true }).fill(password); await page.getByRole('button', { name: '登录', exact: true }).click();
    const record = page.getByLabel('范围撤单记录', { exact: true });
    const counts = page.getByRole('region', { name: '当前范围计数', exact: true });
    async function field(container, name, expected) {
      const cell = container.getByText(name, { exact: true }).locator('..').locator('dd');
      await cell.filter({ hasText: expected }).waitFor(); assert.equal(await cell.textContent(), expected);
    }
    async function screenshot(name, width, height) {
      await page.setViewportSize({ width, height }); assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
      await page.screenshot({ path: `${artifacts}/${name}.png`, fullPage: true });
    }
    if (phase === 'request') {
      stage = 'actual-strategy-entry'; await page.getByRole('link', { name: '查看范围紧急撤单 ↗', exact: true }).click();
      await page.getByRole('heading', { name: '策略范围撤单', exact: true }).waitFor();
      const panel = page.getByRole('region', { name: '策略范围确认', exact: true });
      await panel.getByRole('button', { name: '读取并确认撤单范围', exact: true }).click();
      await field(panel, '策略 ID', strategy); await field(panel, '确认的风险修订', '1');
      await panel.getByLabel('范围撤单原因', { exact: true }).fill(reason);
      await panel.getByRole('checkbox', { name: '我确认关闭此策略新增风险并撤销范围内订单', exact: true }).check();
      const confirm = panel.getByRole('button', { name: '确认范围紧急撤单', exact: true });
      assert.equal(await confirm.isDisabled(), true);
      await panel.getByLabel('确认策略 ID', { exact: true }).fill('01'); assert.equal(await confirm.isDisabled(), true);
      await panel.getByLabel('确认策略 ID', { exact: true }).fill(strategy);
      await screenshot('emergency-confirmation-mobile', 390, 844); await page.setViewportSize({ width: 1440, height: 1000 });
      let lost = false;
      await page.route(new URL(submitPath, origin).href, async route => {
        if (route.request().method() !== 'POST' || lost) { await route.continue(); return; }
        lost = true; const response = await route.fetch(); assert.equal(response.status(), 202); await route.abort();
      });
      stage = 'actual-committed-response-loss'; await confirm.click();
      await panel.getByText('尚未确认范围受理结果，请按原请求重试；离开页面不会撤回已受理任务。', { exact: true }).waitFor();
      assert.equal(await panel.getByLabel('范围撤单原因', { exact: true }).isDisabled(), true);
      assert.equal(await panel.getByLabel('确认策略 ID', { exact: true }).isDisabled(), true);
      assert.equal(await panel.getByRole('button', { name: '取消范围确认', exact: true }).count(), 0);
      assert.equal(await page.getByRole('button', { name: '刷新范围历史', exact: true }).isDisabled(), true);
      assert.equal(await page.getByLabel('每页范围记录', { exact: true }).isDisabled(), true);
      await panel.getByRole('button', { name: '按原请求重试范围撤单', exact: true }).click();
      await page.getByRole('heading', { name: '范围撤单结果', exact: true }).waitFor();
      await field(record, '策略 ID', strategy); await field(record, '操作原因', reason);
      await record.getByText('已受理，待归结', { exact: true }).waitFor();
      assert.equal(writes.length, 2); assert.deepEqual(writes[0], writes[1]); assert.equal(writes[0].path, submitPath); assert.ok(writes[0].key);
      const command = JSON.parse(writes[0].body); assert.deepEqual(Object.keys(command).sort(), ['expected_risk_revision', 'reason', 'scope']);
      assert.equal(command.expected_risk_revision, '1'); assert.equal(command.scope, 'strategy'); assert.equal(command.reason, reason);
      assert.equal(await page.evaluate(() => '__emergencyExecuted' in window), false); assert.equal(await page.locator('img[src="x"]').count(), 0);
      await screenshot('emergency-request-desktop', 1440, 1000); await screenshot('emergency-request-mobile', 390, 844);
    } else {
      stage = `${phase}-actual-proof`; await page.getByRole('heading', { name: '范围撤单结果', exact: true }).waitFor();
      await field(record, '范围记录 ID', receipt); await field(record, '操作原因', reason); await field(counts, '已收录目标', '2');
      assert.equal(await page.getByRole('button', { name: '确认范围紧急撤单', exact: true }).count(), 0);
      if (phase === 'pending') {
        await record.getByText('已受理，待归结', { exact: true }).waitFor(); await field(counts, '当前已归结目标', '0');
        assert.equal(await record.getByText('完成时间（UTC）', { exact: true }).count(), 0);
        await screenshot('emergency-pending', 1440, 1000);
      } else {
        await record.getByText('范围已归结', { exact: true }).waitFor(); await field(counts, '当前已归结目标', '2');
        stage = 'actual-deep-link-refresh'; await page.reload(); await record.getByText('范围已归结', { exact: true }).waitFor();
        await screenshot('emergency-terminal-desktop', 1440, 1000); await screenshot('emergency-terminal-tablet', 800, 1000); await screenshot('emergency-terminal-mobile', 390, 844);
        stage = 'actual-viewer-history'; await page.getByRole('link', { name: '← 范围撤单历史', exact: true }).click();
        await page.getByRole('heading', { name: '策略范围撤单', exact: true }).waitFor();
        assert.equal(await page.getByRole('button', { name: '读取并确认撤单范围', exact: true }).count(), 0);
        await page.getByLabel('每页范围记录', { exact: true }).selectOption('1');
        await page.getByRole('link', { name: `查看范围记录 ${receipt}`, exact: true }).waitFor(); await page.reload();
        assert.match(page.url(), /limit=1/); await page.getByRole('link', { name: `查看范围记录 ${receipt}`, exact: true }).click();
        await field(counts, '当前已归结目标', '2');
        stage = 'protocol-high-integer-and-time';
        await page.route(new URL(resultPath, origin).href, async route => {
          const response = await route.fetch(); const body = await response.json();
          body.record.revision = '9007199254740993'; body.record.acceptance.expected_risk_revision = '9007199254740995'; body.record.acceptance.risk_revision = '9007199254740996';
          body.progress.targets = '9007199254740997'; body.progress.terminal = '9007199254740997'; body.progress.pending_submissions = '9007199254740999'; body.observed_at_ms = '9223372036854775807';
          await route.fulfill({ response, json: body });
        });
        await page.getByRole('button', { name: '刷新范围结果', exact: true }).click(); await field(record, '任务修订', '9007199254740993');
        await field(counts, '已收录目标', '9007199254740997'); await field(counts, '当前待确认新单动作', '9007199254740999'); await field(counts, '采样时间（UTC）', '9223372036854775807');
        await page.unroute(new URL(resultPath, origin).href);
        for (const mutate of [body => { body.progress.targets = 2; }, body => { body.progress.failed = '1'; }, body => { body.record.acceptance.scope = 'account'; }, body => { body.record.private = 'hidden'; }]) {
          stage = 'protocol-invalid-success'; await page.route(new URL(resultPath, origin).href, async route => { const response = await route.fetch(); const body = await response.json(); mutate(body); await route.fulfill({ response, json: body }); });
          await page.getByRole('button', { name: '刷新范围结果', exact: true }).click(); await page.getByText('范围结果暂时不可用，请重新查询。', { exact: true }).waitFor(); assert.equal(await record.count(), 0); assert.equal(await counts.count(), 0);
          await page.unroute(new URL(resultPath, origin).href);
          await page.getByRole('button', { name: '刷新范围结果', exact: true }).click(); await field(counts, '当前已归结目标', '2');
        }
        await page.getByRole('button', { name: '刷新范围结果', exact: true }).click(); await field(counts, '当前已归结目标', '2');
        stage = 'actual-logout-old-cookie'; const cookies = await context.cookies(origin.href); const csrf = cookies.find(cookie => cookie.name === 'muskdeer_csrf'); assert.ok(csrf);
        const response = await context.request.post(new URL('/api/v1/auth/logout', origin).href, { headers: { Origin: origin.origin, 'X-CSRF-Token': csrf.value } }); assert.equal(response.status(), 204); await context.addCookies(cookies);
        await page.getByRole('button', { name: '刷新范围结果', exact: true }).click(); await page.getByRole('heading', { name: '登录工作台', exact: true }).waitFor(); assert.equal(await record.count(), 0);
      }
      assert.deepEqual(writes, []);
    }
    assert.deepEqual(errors, []); assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0); await context.close();
  } finally { await browser.close(); }
}
run().catch(error => { const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error'; console.error(`Emergency browser failed: stage=${stage} category=${category}`); process.exitCode = 1; });
