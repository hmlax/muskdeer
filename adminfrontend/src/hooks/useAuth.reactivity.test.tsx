import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import { useAuthStore } from '@/stores/auth';
import { useAuth } from './useAuth';

vi.mock('@/stores/auth', async () => {
  const { create } = await import('zustand');
  return { useAuthStore: create(() => ({ loginLoading: false })) };
});
vi.mock('@/api/hooks', () => ({ fetchUserProfile: vi.fn(), fetchGenerateCaptcha: vi.fn() }));
vi.mock('@/api/hooks/admin-portal', () => ({ fetchMyPermissionCode: vi.fn() }));

afterEach(() => {
  useAuthStore.setState({ loginLoading: false });
  vi.unstubAllGlobals();
});

it('updates components when store login loading changes', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  const host = document.createElement('div');
  const root = createRoot(host);
  function Indicator() {
    return <span>{useAuth().loginLoading ? 'loading' : 'ready'}</span>;
  }
  try {
    await act(async () => {
      root.render(<Indicator />);
    });
    expect(host.textContent).toBe('ready');
    await act(async () => {
      useAuthStore.setState({ loginLoading: true });
    });
    expect(host.textContent).toBe('loading');
    await act(async () => {
      useAuthStore.setState({ loginLoading: false });
    });
    expect(host.textContent).toBe('ready');
  } finally {
    await act(async () => {
      root.unmount();
    });
  }
});
