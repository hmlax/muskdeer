import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { chromium } from 'playwright';

const origin = new URL(process.env.MUSKDEER_BROWSER_ORIGIN);
assert.equal(origin.hostname, '127.0.0.1');
const artifacts = process.env.MUSKDEER_BROWSER_ARTIFACTS;
await mkdir(artifacts, { recursive: true, mode: 0o700 });
const browser = await chromium.launch({ headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, locale: 'zh-CN' });
  await context.route('**/*', route => new URL(route.request().url()).origin === origin.origin ? route.continue() : route.abort());
  const page = await context.newPage();
  page.setDefaultTimeout(5000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.name));
  await page.goto(new URL('/login', origin).href);
  await page.getByLabel('用户名', { exact: true }).fill(process.env.MUSKDEER_BROWSER_USERNAME);
  await page.getByLabel('密码', { exact: true }).fill(process.env.MUSKDEER_BROWSER_PASSWORD);
  await page.getByRole('button', { name: '登录', exact: true }).click();
  await page.getByRole('heading', { name: `你好，${process.env.MUSKDEER_BROWSER_USERNAME}` }).waitFor();
  await page.getByRole('link', { name: '运行概览', exact: true }).click();
  await page.getByRole('heading', { name: '运行概览', exact: true }).waitFor();
  const selector = page.getByLabel('服务角色', { exact: true });
  assert.deepEqual(await selector.locator('option').evaluateAll(options => options.map(option => option.value)), ['admin', 'operator']);
  assert.equal(await selector.inputValue(), 'operator');
  const services = page.getByRole('region', { name: '服务运行观测', exact: true });
  await services.getByRole('heading', { name: 'business-operator', exact: true }).waitFor();
  await selector.selectOption('admin');
  await page.getByRole('button', { name: '查询服务角色', exact: true }).click();
  await services.getByRole('heading', { name: 'two-apps-admin', exact: true }).waitFor();
  await services.getByText('此次就绪', { exact: true }).waitFor();
  await page.screenshot({ path: `${artifacts}/overview-admin-operator.png`, fullPage: true });
  assert.deepEqual(errors, []);
  await context.close();
  console.log('React: independent admin/operator overview and current readiness passed');
} finally {
  await browser.close();
}
