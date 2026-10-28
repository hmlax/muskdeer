import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  const id = process.env.MUSKDEER_BROWSER_STRATEGY_ID;
  const phase = process.env.MUSKDEER_BROWSER_CONTROL_PHASE;
  const operationID = process.env.MUSKDEER_BROWSER_OPERATION_ID;
  const username = process.env.MUSKDEER_BROWSER_USERNAME;
  const password = process.env.MUSKDEER_BROWSER_PASSWORD;
  const artifacts = process.env.MUSKDEER_BROWSER_ARTIFACTS;
  const titles = { start: '启动', pause: '暂停', resume: '恢复', stop: '停止' };
  const targets = { start: '运行中', pause: '已暂停', resume: '运行中', stop: '已停止' };
  assert.equal(origin.protocol, 'http:');
  assert.equal(origin.hostname, '127.0.0.1');
  assert.equal(origin.pathname, '/');
  assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  assert.ok(/^[1-9][0-9]*$/.test(id ?? '') && artifacts && username && password && (Object.hasOwn(titles, phase) || phase === 'viewer'));
  await mkdir(artifacts, { recursive: true, mode: 0o700 });
  const browser = await chromium.launch({ headless: true });
  try {
    const mobile = phase === 'pause';
    const context = await browser.newContext({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
    const page = await context.newPage();
    page.setDefaultTimeout(10000);
    const errors = [];
    const keys = [];
    page.on('pageerror', error => errors.push(error.name));
    page.on('request', request => {
      if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/control')) keys.push(request.headers()['idempotency-key']);
    });
    stage = 'actual-login';
    await page.goto(new URL(phase === 'viewer' ? `/operations/${operationID}` : `/strategies/${id}`, origin).href);
    await page.getByLabel('用户名', { exact: true }).fill(username);
    await page.getByLabel('密码', { exact: true }).fill(password);
    await page.getByRole('button', { name: '登录', exact: true }).click();
    if (phase === 'viewer') {
      stage = 'viewer-result-and-controls';
      await page.getByRole('region', { name: '操作记录' }).getByText('已完成', { exact: true }).waitFor();
      await page.reload();
      await page.getByRole('region', { name: '操作记录' }).getByText('已停止', { exact: true }).first().waitFor();
      await page.getByRole('link', { name: '← 策略详情', exact: true }).click();
      const controls = page.getByRole('region', { name: '运行控制', exact: true });
      await controls.getByText('当前账户为只读用户，可查看状态与操作结果。').waitFor();
      assert.equal(await controls.getByRole('button').count(), 0);
      assert.deepEqual(keys, []);
      await page.screenshot({ path: `${artifacts}/control-viewer.png`, fullPage: true });
    } else {
      stage = `${phase}-form`;
      const controls = page.getByRole('region', { name: '运行控制', exact: true });
      await controls.getByRole('button', { name: titles[phase], exact: true }).click();
      if (phase === 'pause') {
        await controls.getByRole('button', { name: `确认${titles[phase]}`, exact: true }).click();
        assert.deepEqual(keys, []);
      }
      await page.getByLabel('操作原因', { exact: true }).fill(`browser ${phase} with actual funded orders`);
      if (phase === 'pause') await page.screenshot({ path: `${artifacts}/control-pause-form-mobile.png`, fullPage: true });
      if (phase === 'start') {
        stage = 'committed-response-loss';
        let lost = false;
        await page.route(`**/api/v1/strategies/${id}/control`, async route => {
          if (lost) { await route.continue(); return; }
          lost = true;
          const accepted = await route.fetch();
          assert.equal(accepted.status(), 202);
          await route.abort();
        });
        await controls.getByRole('button', { name: `确认${titles[phase]}`, exact: true }).click();
        await controls.getByRole('alert').filter({ hasText: '尚未确认此请求的受理结果' }).waitFor();
        await page.screenshot({ path: `${artifacts}/control-response-unknown.png`, fullPage: true });
        await controls.getByRole('button', { name: '按原请求重试', exact: true }).click();
      } else if (phase === 'pause') {
        stage = 'csrf-rejection-and-original-retry';
        const csrf = (await context.cookies()).find(cookie => cookie.name === 'muskdeer_csrf');
        assert.ok(csrf);
        await context.addCookies([{ ...csrf, value: Buffer.alloc(32, 7).toString('base64url') }]);
        const forbidden = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname.endsWith('/control'));
        await controls.getByRole('button', { name: `确认${titles[phase]}`, exact: true }).click();
        assert.equal((await forbidden).status(), 403);
        await controls.getByRole('alert').filter({ hasText: '此操作未获授权' }).waitFor();
        await context.addCookies([csrf]);
        await controls.getByRole('button', { name: '按原请求重试', exact: true }).click();
      } else await controls.getByRole('button', { name: `确认${titles[phase]}`, exact: true }).click();
      stage = `${phase}-actual-completion`;
      const receipt = page.getByRole('region', { name: '最新操作', exact: true });
      await receipt.getByText('已完成', { exact: true }).waitFor();
      await receipt.getByText(targets[phase], { exact: true }).first().waitFor();
      assert.ok(keys.length >= 1 && keys.every(key => key && key === keys[0]));
      if (phase === 'start') assert.ok(keys.length >= 2);
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
      await page.screenshot({ path: `${artifacts}/control-${phase}.png`, fullPage: true });
      await receipt.getByRole('link', { name: '查看操作结果 ↗', exact: true }).click();
      await page.getByRole('heading', { name: '操作结果', level: 2 }).waitFor();
      await page.getByRole('region', { name: '操作记录', exact: true }).getByText('已完成', { exact: true }).waitFor();
      await page.reload();
      await page.getByRole('region', { name: '操作记录', exact: true }).getByText('已完成', { exact: true }).waitFor();
      if (phase === 'stop') await page.screenshot({ path: `${artifacts}/control-operation-result.png`, fullPage: true });
    }
    assert.deepEqual(await page.evaluate(() => [localStorage.length, sessionStorage.length]), [0, 0]);
    assert.deepEqual(errors, []);
    await context.close();
    console.log(`Actual funded control browser phase passed: ${phase}`);
  } finally { await browser.close(); }
}

try { await run(); }
catch (error) {
  console.error(`Control browser failed: stage=${stage} category=${error instanceof Error ? error.name : 'unknown'}`);
  process.exitCode = 1;
}
