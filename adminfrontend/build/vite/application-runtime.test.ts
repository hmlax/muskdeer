// @vitest-environment node
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import type { AddressInfo } from 'node:net';
import { afterEach, expect, it, vi } from 'vitest';
import { createServer } from 'vite';

afterEach(() => vi.unstubAllEnvs());

it.each(['platform', 'merchant'])(
  '%s serves the shared application with a browser-compatible JSX runtime',
  async (kind) => {
    vi.stubEnv('NODE_ENV', 'development');
    vi.stubEnv('VITE_MERCHANT_CODE', 'RuntimeTest');
    vi.stubEnv('VITE_AES_KEY', 'test-only-key-12');
    const cacheDir = await mkdtemp(path.join(tmpdir(), 'admin-runtime-'));
    const server = await createServer({
      configFile: fileURLToPath(new URL(`../../${kind}-admin/vite.config.ts`, import.meta.url)),
      cacheDir,
      logLevel: 'silent',
      server: { port: 0, host: '127.0.0.1', hmr: false },
      optimizeDeps: { noDiscovery: true },
    });
    try {
      await server.listen();
      const port = (server.httpServer?.address() as AddressInfo).port;
      const base = `http://127.0.0.1:${port}`;
      const entry = fileURLToPath(
        new URL('../../src/pages/app/dashboard/analytics/index.tsx', import.meta.url),
      );
      const response = await fetch(`${base}/@fs${entry}`);
      expect(response.status).toBe(200);
      const source = await response.text();
      expect(source).not.toMatch(/\.pnpm\/react@[^"']+\/react\/jsx-runtime\.js/);
      const runtimePath = source.match(/"([^"\n]+\/deps\/react_jsx(?:-dev)?-runtime\.js[^"\n]*)"/);
      expect(runtimePath, 'JSX runtime must be served as optimized ESM').not.toBeNull();
      const runtime = await fetch(`${base}${runtimePath![1]}`);
      expect(runtime.status).toBe(200);
      expect(await runtime.text()).toMatch(/export\s/);
    } finally {
      await server.close();
      await rm(cacheDir, { recursive: true, force: true });
    }
  },
  30_000,
);
