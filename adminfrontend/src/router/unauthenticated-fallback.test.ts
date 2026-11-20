import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { createMemoryRouter, Outlet, RouterProvider } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { errorRoutes } from './config/error-routes';

const authState = vi.hoisted(() => ({ accessToken: null as string | null, mfaOperationId: null }));
vi.mock('@/stores', () => ({ useAuthStore: (selector: (state: typeof authState) => unknown) => selector(authState) }));
vi.mock('@/layouts/BlankLayout', () => ({ default: Outlet }));
vi.mock('@/layouts/components/ErrorFallback/RouteErrorFallback', () => ({ default: () => null }));
vi.mock('@/pages/core/error', () => ({
  Unauthorized: () => null, Forbidden: () => null, InternalError: () => null,
  Offline: () => null, ComingSoon: () => null, NotFound: () => createElement('div', null, 'Not found'),
}));

let root: Root;
let container: HTMLDivElement;
let router: ReturnType<typeof createMemoryRouter>;
beforeEach(() => {
  vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
  authState.accessToken = null;
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  router.dispose();
  container.remove();
  vi.unstubAllGlobals();
});

async function render() {
  router = createMemoryRouter([
    { path: '/auth/login', element: createElement('div', null, 'Login') },
    ...errorRoutes,
  ], { initialEntries: ['/dashboard/analytics'] });
  await act(async () => { root.render(createElement(RouterProvider, { router })); });
}

it('sends an expired merchant session to login before server routes have loaded', async () => {
  await render();
  expect(router.state.location.pathname).toBe('/auth/login');
  expect(new URLSearchParams(router.state.location.search).get('redirect')).toBe('/dashboard/analytics');
});

it('keeps an authenticated unknown page as 404', async () => {
  authState.accessToken = 'test-token';
  await render();
  expect(router.state.location.pathname).toBe('/dashboard/analytics');
  expect(container.textContent).toBe('Not found');
});
