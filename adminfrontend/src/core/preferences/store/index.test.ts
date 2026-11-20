import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { defaultPreferences } from '../config/default';
import { applicationStorageKey } from '@/config/application';

beforeEach(() => {
  const values = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  });
  vi.resetModules();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

it('updates cached branding while preserving theme and layout preferences', async () => {
  localStorage.setItem(applicationStorageKey('preferences'), JSON.stringify({
    version: 2,
    state: {
      preferences: {
        ...defaultPreferences,
        app: { ...defaultPreferences.app, name: 'Previous Admin', layout: 'top-nav' },
        copyright: {
          ...defaultPreferences.copyright,
          companyName: 'Previous Company',
          companySiteLink: 'https://example.com',
        },
        theme: { ...defaultPreferences.theme, mode: 'light', colorPrimary: '#123456' },
      },
    },
  }));

  const { usePreferencesStore } = await import('./index');
  const { preferences } = usePreferencesStore.getState();

  expect(preferences.app.name).toBe('Go Admin');
  expect(preferences.copyright.companyName).toBe('Go Admin');
  expect(preferences.copyright.companySiteLink).toBe('https://github.com/hmlax/admin');
  expect(preferences.app.layout).toBe('top-nav');
  expect(preferences.theme.mode).toBe('light');
  expect(preferences.theme.colorPrimary).toBe('#123456');
});

it('preserves branding saved after the migration', async () => {
  localStorage.setItem(applicationStorageKey('preferences'), JSON.stringify({
    version: 3,
    state: {
      preferences: {
        ...defaultPreferences,
        app: { ...defaultPreferences.app, name: 'Custom Admin' },
        copyright: { ...defaultPreferences.copyright, companyName: 'Custom Company' },
      },
    },
  }));

  const { usePreferencesStore } = await import('./index');
  const { preferences } = usePreferencesStore.getState();

  expect(preferences.app.name).toBe('Custom Admin');
  expect(preferences.copyright.companyName).toBe('Custom Company');
});
