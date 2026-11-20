import {
  AxiosError,
  AxiosHeaders,
  type AxiosAdapter,
  type AxiosResponse,
  type InternalAxiosRequestConfig,
} from 'axios';
import { describe, expect, it, vi } from 'vitest';
import { RequestClient } from './request-client';

function response(
  config: InternalAxiosRequestConfig,
  data: unknown,
  status = 200,
): AxiosResponse<unknown> {
  return { config, data, status, statusText: String(status), headers: new AxiosHeaders() };
}

describe('RequestClient response and refresh flow', () => {
  it('unwraps successful responses once, including requests without application callbacks', async () => {
    const adapter: AxiosAdapter = async (config) =>
      response(config, { status: 'business-status', value: 0 });
    const client = new RequestClient({ adapter });
    expect(await client.get('/test')).toEqual({ status: 'business-status', value: 0 });
    const raw = await client.requestResponse('/test', { method: 'GET' });
    expect(raw.status).toBe(200);
  });

  it('refreshes once for concurrent 401s and preserves the retried response body', async () => {
    let token = 'old';
    let release: () => void = () => {
      throw new Error('Refresh has not started');
    };
    const refreshed = new Promise<void>((resolve) => {
      release = resolve;
    });
    const refreshToken = vi.fn(async () => {
      await refreshed;
      token = 'new';
      return token;
    });
    const onError = vi.fn();
    const adapter: AxiosAdapter = async (config) => {
      if (config.headers.Authorization !== 'Bearer new') {
        throw new AxiosError(
          'unauthorized',
          'ERR_BAD_REQUEST',
          config,
          undefined,
          response(config, { reason: 'UNAUTHORIZED' }, 401),
        );
      }
      return response(config, { url: config.url, status: 'business-status' });
    };
    const client = new RequestClient(
      { adapter },
      {
        getToken: () => token,
        refreshToken,
        onReAuthenticate: vi.fn(),
        onError,
      },
    );
    const pending = Promise.all([client.get('/one'), client.get('/two'), client.get('/three')]);
    await vi.waitFor(() => expect(client.refreshTokenQueue).toHaveLength(2));
    release();
    expect(await pending).toEqual([
      { url: '/one', status: 'business-status' },
      { url: '/two', status: 'business-status' },
      { url: '/three', status: 'business-status' },
    ]);
    expect(refreshToken).toHaveBeenCalledOnce();
    expect(client.refreshTokenQueue).toHaveLength(0);
    expect(client.isRefreshing).toBe(false);
    expect(onError).not.toHaveBeenCalled();
  });

  it('preserves server error fields while translating the displayed message', async () => {
    const adapter: AxiosAdapter = async (config) => {
      throw new AxiosError(
        'failed',
        'ERR_BAD_REQUEST',
        config,
        undefined,
        response(config, { reason: 'DENIED', code: 403, message: 'server' }, 403),
      );
    };
    const onError = vi.fn();
    const client = new RequestClient(
      { adapter },
      {
        getToken: () => null,
        refreshToken: vi.fn(),
        onReAuthenticate: vi.fn(),
        getErrorMsg: () => 'translated',
        onError,
      },
    );
    await expect(client.get('/denied')).rejects.toEqual({
      reason: 'DENIED',
      code: 403,
      message: 'translated',
    });
    expect(onError).toHaveBeenCalledWith('translated');
  });

  it('rejects a second 401 after refresh without looping or losing the handled marker', async () => {
    const adapter: AxiosAdapter = async (config) => {
      throw new AxiosError(
        'unauthorized',
        'ERR_BAD_REQUEST',
        config,
        undefined,
        response(config, {}, 401),
      );
    };
    const onReAuthenticate = vi.fn();
    const onError = vi.fn();
    const refreshToken = vi.fn(async () => 'new');
    const client = new RequestClient(
      { adapter },
      { getToken: () => 'old', refreshToken, onReAuthenticate, onError },
    );
    await expect(client.get('/private')).rejects.toMatchObject({
      __handledByAuthInterceptor: true,
    });
    expect(refreshToken).toHaveBeenCalledOnce();
    expect(onReAuthenticate).toHaveBeenCalledOnce();
    expect(onError).not.toHaveBeenCalled();
  });
});
