import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { restoreApplicationTheme } from './appearance';

beforeEach(() => {
  const values = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
});

it('restores the scoped theme without reading the shared or other application preferences', () => {
  const preferences = (mode: string) =>
    JSON.stringify({ state: { preferences: { theme: { mode } } } });
  localStorage.setItem('app-preferences', preferences('light'));
  localStorage.setItem('platform-admin:preferences-storage', preferences('light'));
  localStorage.setItem('merchant-admin:MerchantA:preferences-storage', preferences('dark'));
  restoreApplicationTheme('merchant-admin:MerchantA:preferences-storage');
  expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  restoreApplicationTheme('platform-admin:preferences-storage');
  expect(document.documentElement.getAttribute('data-theme')).toBe('light');
});

it('resolves the saved automatic theme before React loads', () => {
  vi.stubGlobal('matchMedia', () => ({ matches: true }));
  localStorage.setItem(
    'theme',
    JSON.stringify({ state: { preferences: { theme: { mode: 'auto' } } } }),
  );
  restoreApplicationTheme('theme');
  expect(document.documentElement.getAttribute('data-theme')).toBe('light');
});
