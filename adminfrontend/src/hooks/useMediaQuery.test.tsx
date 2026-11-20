import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
import { useMediaQuery } from './useMediaQuery';

it('reads current browser media state and follows changes without resetting component state', async () => {
  const media = Object.assign(new EventTarget(), { matches: false });
  vi.stubGlobal('matchMedia', () => media);
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  const container = document.createElement('div');
  const root = createRoot(container);
  function View() {
    return String(useMediaQuery('(prefers-color-scheme: dark)'));
  }
  try {
    await act(async () => {
      root.render(createElement(View));
    });
    expect(container.textContent).toBe('false');
    await act(async () => {
      media.matches = true;
      media.dispatchEvent(new Event('change'));
    });
    expect(container.textContent).toBe('true');
  } finally {
    await act(async () => {
      root.unmount();
    });
    vi.unstubAllGlobals();
  }
});
