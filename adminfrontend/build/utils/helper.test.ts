// @vitest-environment node
import { fileURLToPath } from 'node:url';
import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import { loadEnv } from 'vite';
import axios from 'axios';

import { handleEnv } from './helper';
import { createProxy } from '../vite/proxy';

const root = fileURLToPath(new URL('../../platform-admin/', import.meta.url));

describe('frontend environment', () => {
  beforeEach(() => {
    vi.stubEnv('VITE_AES_KEY', 'test-only-key-12');
  });
  afterEach(() => vi.unstubAllEnvs());
  it.each([
    ['platform', 'development'],
    ['platform', 'production'],
    ['merchant', 'development'],
    ['merchant', 'production'],
  ])('loads the required %s %s settings, including local preview proxies', (kind, mode) => {
    const appRoot = fileURLToPath(new URL(`../../${kind}-admin/`, import.meta.url));
    const env = loadEnv(mode, appRoot, 'VITE_');
    expect(() => handleEnv(env)).not.toThrow();
    expect(env.VITE_APP_TITLE).toContain('Muskdeer');
    expect(env.VITE_AES_KEY).toHaveLength(16);
    expect(handleEnv(env).VITE_PROXY).toHaveLength(1);
  });

  it('keeps refresh-token requests at the API root when restoring a nested page', () => {
    const env = loadEnv('development', root, 'VITE_');
    const config = handleEnv(env);
    const uri = axios.getUri({ baseURL: env.VITE_API_URL, url: 'admin/v1/refresh-token' });
    const target = new URL(uri, 'http://localhost:15888/dashboard/analytics');

    expect(target.pathname).toBe('/admin/v1/refresh-token');
    expect(config.VITE_SERVER_PORT).toBe(15888);
    expect(createProxy(config.VITE_PROXY)['^/admin']).toMatchObject({
      target: 'http://127.0.0.1:17788/',
      changeOrigin: true,
    });
    expect(createProxy(config.VITE_PROXY)['^/admin'].rewrite).toBeUndefined();
  });

  it('stops startup with the missing variable names when the environment files are absent', () => {
    expect(() => handleEnv({})).toThrow(/VITE_API_URL.*VITE_APP_TITLE.*VITE_AES_KEY/);
  });

  it.each(['', 'MerchantA'])('pins the portal header for API proxies (%s)', (code) => {
    const proxies = createProxy([['/admin', 'http://127.0.0.1:7788']], code);
    for (const proxy of Object.values(proxies)) {
      const on = vi.fn();
      proxy.configure?.({ on } as never, {} as never);
      const request = { setHeader: vi.fn(), removeHeader: vi.fn() };
      on.mock.calls[0][1](request);
      if (code) expect(request.setHeader).toHaveBeenCalledWith('X-Tenant-Code', code);
      else expect(request.removeHeader).toHaveBeenCalledWith('X-Tenant-Code');
    }
  });
});
