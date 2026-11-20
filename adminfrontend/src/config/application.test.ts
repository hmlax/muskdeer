import { afterEach, describe, expect, it, vi } from 'vitest';

afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
  vi.resetModules();
});

async function loadApplication(kind: 'platform' | 'merchant', code: string) {
  vi.stubEnv('VITE_ADMIN_APP', kind);
  vi.stubEnv('DEV', false);
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue(new Response(JSON.stringify({ merchantCode: code }))),
  );
  const app = await import('./application');
  await app.initializeApplication();
  return app;
}

describe('independent admin deployments', () => {
  it('platform has no merchant header even when deployment data contains a code', async () => {
    const app = await loadApplication('platform', 'MerchantA');
    expect(app.applicationHeaders()).toEqual({});
    expect(app.refreshExpiryCookieName()).toBe('refresh_exp');
    expect(fetch).not.toHaveBeenCalled();
  });

  it('merchant uses runtime deployment configuration for headers, storage and cookies', async () => {
    const app = await loadApplication('merchant', 'MerchantA');
    expect(app.applicationHeaders()).toEqual({ 'X-Tenant-Code': 'MerchantA' });
    expect(app.applicationStorageKey('auth')).toBe('merchant-admin:MerchantA:auth-storage');
    expect(app.refreshExpiryCookieName()).toBe('refresh_exp_MerchantA');
  });

  it('an unconfigured merchant deployment cannot become a platform login', async () => {
    await expect(loadApplication('merchant', '')).rejects.toThrow(/merchantCode/);
  });

  it.each(['one,two', 'one\n', ' one', 'a'.repeat(129)])(
    'rejects the invalid merchant code %j',
    async (code) => {
      await expect(loadApplication('merchant', code)).rejects.toThrow(/merchantCode/);
    },
  );

  it('keeps UI caches separate between the platform and each merchant', async () => {
    const names = ['preferences', 'tabs', 'lock', 'locale', 'menu-search', 'bi-schema'];
    const platform = await loadApplication('platform', '');
    const platformKeys = names.map(platform.applicationStorageKey);
    vi.resetModules();
    const first = await loadApplication('merchant', 'MerchantA');
    const firstKeys = names.map(first.applicationStorageKey);
    vi.resetModules();
    const second = await loadApplication('merchant', 'MerchantB');
    const secondKeys = names.map(second.applicationStorageKey);

    expect(new Set([...platformKeys, ...firstKeys, ...secondKeys]).size).toBe(18);
  });

  it('does not start a merchant when the runtime configuration endpoint fails', async () => {
    vi.stubEnv('VITE_ADMIN_APP', 'merchant');
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 503 })));
    const app = await import('./application');
    await expect(app.initializeApplication()).rejects.toThrow('503');
    expect(() => app.applicationHeaders()).toThrow(/merchantCode/);
  });

  it('uses the runtime endpoint in development too', async () => {
    vi.stubEnv('VITE_ADMIN_APP', 'merchant');
    vi.stubEnv('DEV', true);
    vi.stubEnv('VITE_MERCHANT_CODE', 'WrongMerchant');
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response(JSON.stringify({ merchantCode: 'MerchantA' }))),
    );
    const app = await import('./application');
    await app.initializeApplication();
    expect(fetch).toHaveBeenCalledWith('/app-config.json', { cache: 'no-store' });
    expect(app.getMerchantCode()).toBe('MerchantA');
  });
});
