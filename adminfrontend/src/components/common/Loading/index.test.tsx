import { act, type ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import { ThemeLoading } from './index';

vi.mock('@/core/preferences', () => ({
  usePreferencesStore: (
    selector: (state: { preferences: { theme: { mode: string } } }) => unknown,
  ) => selector({ preferences: { theme: { mode: 'light' } } }),
  useThemeConfig: () => ({ token: { colorPrimary: '#006be6' } }),
}));
vi.mock('@/core/i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }));
vi.mock('antd', () => ({
  ConfigProvider: ({ children }: { children: ReactNode }) => children,
  Spin: () => <span>loading</span>,
  Progress: () => <span>progress</span>,
}));

it('can hide at completion and show again without changing hook order', async () => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  const container = document.createElement('div');
  const root = createRoot(container);
  try {
    await act(async () => {
      root.render(<ThemeLoading progress={100} />);
    });
    expect(container.textContent).toBe('');
    await act(async () => {
      root.render(<ThemeLoading progress={50} />);
    });
    expect(container.textContent).toContain('loading');
    await act(async () => {
      root.render(<ThemeLoading progress={100} status="success" />);
    });
    expect(container.textContent).toContain('loading.success');
  } finally {
    await act(async () => {
      root.unmount();
    });
    vi.unstubAllGlobals();
  }
});
