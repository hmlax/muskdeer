import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

let stage = 'configuration';
async function run() {
  const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN ?? '');
  assert.equal(origin.protocol, 'http:');
  assert.equal(origin.hostname, '127.0.0.1');
  assert.equal(origin.pathname, '/');
  assert.equal(origin.username + origin.password + origin.search + origin.hash, '');
  const username = process.env.MUSKDEER_BROWSER_USERNAME;
  const password = process.env.MUSKDEER_BROWSER_PASSWORD;
  const artifacts = process.env.MUSKDEER_BROWSER_ARTIFACTS;
  assert.ok(username && password && artifacts, 'explicit isolated fixture input is required');
  await mkdir(artifacts, { recursive: true, mode: 0o700 });

  stage = 'browser-launch';
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
    await context.route('**/*', route => {
      if (new URL(route.request().url()).origin === origin.origin) return route.continue();
      return route.abort();
    });
    const page = await context.newPage();
    page.setDefaultTimeout(5000);
    const errors = [];
    page.on('pageerror', error => errors.push(error.name));
    stage = 'login-page';
    await page.goto(new URL('/login', origin).href);
    await page.getByRole('heading', { name: '登录工作台' }).waitFor();
    await page.screenshot({ path: `${artifacts}/login-desktop.png`, fullPage: true });

    stage = 'invalid-credentials';
    await page.getByLabel('用户名', { exact: true }).fill(username);
    await page.getByLabel('密码', { exact: true }).fill('test-only-invalid-password');
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await page.getByRole('alert').filter({ hasText: '用户名或密码不正确' }).waitFor();
    stage = 'login';
    await page.getByLabel('密码', { exact: true }).fill(password);
    await page.getByRole('button', { name: '登录', exact: true }).click();
    await page.getByRole('heading', { name: `你好，${username}` }).waitFor();
    assert.equal(new URL(page.url()).pathname, '/session');
    stage = 'session-and-refresh';
    const cookies = await context.cookies();
    const bearer = cookies.find(cookie => cookie.name === 'muskdeer_session');
    const csrf = cookies.find(cookie => cookie.name === 'muskdeer_csrf');
    assert.ok(bearer?.httpOnly && bearer.sameSite === 'Strict' && bearer.path === '/');
    assert.ok(csrf && !csrf.httpOnly && csrf.value !== bearer.value);
    assert.equal(await page.evaluate(() => document.cookie.includes('muskdeer_session=')), false);
    assert.deepEqual(await page.evaluate(() => [localStorage.length, sessionStorage.length]), [0, 0]);
    await page.reload();
    await page.getByRole('heading', { name: `你好，${username}` }).waitFor();
    await page.getByText('只读用户', { exact: true }).waitFor();
    const identity = await context.request.get(new URL('/api/v1/auth/session', origin).href);
    assert.equal(identity.status(), 200);
    const body = await identity.json();
    for (const field of ['user_id', 'session_id', 'expires_at_ms']) assert.match(body.session[field], /^[1-9][0-9]*$/);
    await page.screenshot({ path: `${artifacts}/session-desktop.png`, fullPage: true });
    const missing = await context.request.get(new URL('/api/v1/not-present', origin).href);
    assert.equal(missing.status(), 404);
    assert.equal((await missing.json()).code, 'NOT_FOUND');
    stage = 'logout';
    await page.getByRole('button', { name: '退出当前会话' }).click();
    await page.getByRole('heading', { name: '登录工作台' }).waitFor();
    const replay = await context.request.get(new URL('/api/v1/auth/session', origin).href, {
      headers: { Cookie: `muskdeer_session=${bearer.value}` },
    });
    assert.equal(replay.status(), 401);
    stage = 'mobile';
    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload();
    await page.getByRole('heading', { name: '登录工作台' }).waitFor();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await page.screenshot({ path: `${artifacts}/login-mobile.png`, fullPage: true });
    assert.deepEqual(errors, []);
    await context.close();
    console.log('Actual management browser login, refresh, HTTP-only session, logout and mobile layout passed');
  } finally {
    await browser.close();
  }
}

run().catch(error => {
  // Playwright's raw call logs can contain fill values or Cookie headers.
  // Retain only our fixed stage and a small diagnostic category.
  const category = ['Error', 'AssertionError', 'TimeoutError'].includes(error?.name) ? error.name : 'Error';
  console.error(`Management browser failed: stage=${stage} category=${category}`);
  process.exitCode = 1;
});
