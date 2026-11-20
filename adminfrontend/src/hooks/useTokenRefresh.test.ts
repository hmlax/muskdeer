import { afterEach, beforeEach, expect, it, vi } from 'vitest';

const auth = vi.hoisted(() => ({
  accessTokenExpireAt: 0,
  forceLogout: vi.fn(),
  refreshToken: vi.fn().mockResolvedValue(''),
}));
vi.mock('@/stores/auth', () => ({ useAuthStore: { getState: () => auth } }));
vi.mock('@/api/hooks/user-profile', () => ({ fetchUserProfile: vi.fn() }));

beforeEach(() => {
  vi.resetModules();
  vi.clearAllMocks();
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-11-04T00:00:00Z'));
  auth.accessTokenExpireAt = Date.now() + 10 * 60 * 1000;
});
afterEach(() => {
  for (const name of ['refresh_exp', 'refresh_exp_MerchantA', 'refresh_exp_MerchantB']) {
    document.cookie = `${name}=; Max-Age=0; Path=/`;
  }
  vi.useRealTimers();
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

it('refreshes the merchant session using its own cookie while the platform cookie is expired', async () => {
  vi.stubEnv('VITE_ADMIN_APP', 'merchant');
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(new Response(JSON.stringify({ merchantCode: 'MerchantA' }))),
  );
  const app = await import('@/config/application');
  await app.initializeApplication();
  document.cookie = 'refresh_exp=1; Path=/';
  document.cookie = `refresh_exp_MerchantA=${Math.floor(Date.now() / 1000) + 3600}; Path=/`;
  const timer = await import('./useTokenRefresh');
  timer.startRefreshTimer();
  await vi.advanceTimersByTimeAsync(4 * 60 * 1000);
  timer.stopRefreshTimer();
  expect(auth.refreshToken).toHaveBeenCalledOnce();
  expect(auth.forceLogout).not.toHaveBeenCalled();
});

it('does not borrow another merchant or platform refresh cookie', async () => {
  vi.stubEnv('VITE_ADMIN_APP', 'merchant');
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(new Response(JSON.stringify({ merchantCode: 'MerchantA' }))),
  );
  const app = await import('@/config/application');
  await app.initializeApplication();
  for (const name of ['refresh_exp', 'refresh_exp_MerchantB']) {
    document.cookie = `${name}=${Math.floor(Date.now() / 1000) + 3600}; Path=/`;
  }
  const timer = await import('./useTokenRefresh');
  timer.startRefreshTimer();
  await vi.advanceTimersByTimeAsync(4 * 60 * 1000);
  timer.stopRefreshTimer();
  expect(auth.forceLogout).toHaveBeenCalledOnce();
  expect(auth.refreshToken).not.toHaveBeenCalled();
});
