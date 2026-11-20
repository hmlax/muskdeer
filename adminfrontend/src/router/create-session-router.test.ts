import { createElement } from 'react';
import { matchRoutes, Outlet } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createSessionRouter } from './create-session-router';
import type { GenerateMenuAndRoutesOptions } from '@/core/router/types';
import { transformRoutesToMenu } from '@/core/router/utils/menu';
import { errorRoutes } from './config/error-routes';

vi.mock('@/utils', async () => import('@/utils/tree'));
vi.mock('@/router/guards', () => ({ AuthGuard: ({ children }: { children: unknown }) => children }));
vi.mock('@/components/common/Loading', () => ({ default: () => null }));
vi.mock('@/layouts/components/ErrorFallback/RouteErrorFallback', () => ({ default: () => null }));
vi.mock('@/pages/core/error', () => ({
  Unauthorized: () => null,
  Forbidden: () => null,
  InternalError: () => null,
  Offline: () => null,
  ComingSoon: () => null,
  NotFound: () => null,
}));

const portal = vi.hoisted(() => ({ application: 'platform' }));
vi.mock('@/config/application', () => ({
  get adminApplication() {
    return portal.application;
  },
}));

const routers: Array<{ dispose: () => void }> = [];
afterEach(() => {
  routers.splice(0).forEach((router) => router.dispose());
  portal.application = 'platform';
});

type SidebarItem = { path?: string; children?: SidebarItem[] };
function menuPaths(items: SidebarItem[]): string[] {
  return items.flatMap((item) => [item.path ?? '', ...menuPaths(item.children ?? [])]);
}

function options(): GenerateMenuAndRoutesOptions {
  return {
    routes: [
      {
        path: '/',
        children: [
          { name: 'TenantManagement', path: 'tenant/plans', element: createElement('div') },
        ],
      },
      {
        path: '/opm/profile',
        element: createElement(Outlet),
        meta: { hideInMenu: true },
        children: [{ name: 'user-profile', index: true, element: createElement('div') }],
      },
      { path: '*', element: createElement('div') },
    ],
    permissions: ['sys:tenant_manager'],
    fetchMenuListAsync: vi.fn(async () => [
      {
        name: 'Dashboard',
        path: '/dashboard',
        component: 'BasicLayout',
        children: [
          { name: 'Analytics', path: 'analytics', component: 'dashboard/analytics/index' },
        ],
      },
    ]),
    layoutMap: { BasicLayout: Outlet },
    pageMap: { '/dashboard/analytics/index': () => createElement('div') },
  };
}

describe('session routes respect server tenant scope', () => {
  it('retains the production error pages when unauthorized tenant routes are absent', async () => {
    const config = options();
    config.routes = [...config.routes.filter((route) => route.path !== '*'), ...errorRoutes];
    const bundle = await createSessionRouter(
      'frontend',
      { authenticated: true, user: { tenantId: 16 } },
      config,
    );
    routers.push(bundle.router);
    expect(matchRoutes(bundle.router.routes, '/permission/roles')?.at(-1)?.route.name).toBe(
      'not-found',
    );
    expect(matchRoutes(bundle.router.routes, '/403')?.at(-1)?.route.name).toBe('forbidden');
  });

  it('uses server menus for a tenant even with a saved frontend preference', async () => {
    const config = options();
    const bundle = await createSessionRouter(
      'frontend',
      { authenticated: true, user: { tenantId: 16 } },
      config,
    );
    routers.push(bundle.router);
    expect(config.fetchMenuListAsync).toHaveBeenCalledOnce();
    expect(matchRoutes(bundle.router.routes, '/tenant/plans')?.at(-1)?.route.path).toBe('*');
    expect(matchRoutes(bundle.router.routes, '/dashboard/analytics')?.at(-1)?.route.path).toBe(
      'analytics',
    );
    const paths = menuPaths(transformRoutesToMenu(bundle.routes, config.permissions ?? []) ?? []);
    expect(paths).toContain('/dashboard/analytics');
    expect(paths).not.toContain('/tenant/plans');
    expect(paths).not.toContain('/opm/profile');
    expect(matchRoutes(bundle.router.routes, '/opm/profile')?.at(-1)?.route.index).toBe(true);
  });

  it('does not mount local business routes before authenticated identity is loaded', async () => {
    const config = options();
    const bundle = await createSessionRouter(
      'frontend',
      { authenticated: true, user: null },
      config,
    );
    routers.push(bundle.router);
    expect(config.fetchMenuListAsync).toHaveBeenCalledOnce();
    expect(matchRoutes(bundle.router.routes, '/tenant/plans')?.at(-1)?.route.path).toBe('*');
  });

  it('preserves the platform frontend preference', async () => {
    const config = options();
    const bundle = await createSessionRouter(
      'frontend',
      { authenticated: true, user: { tenantId: 0 } },
      config,
    );
    routers.push(bundle.router);
    expect(config.fetchMenuListAsync).not.toHaveBeenCalled();
    expect(matchRoutes(bundle.router.routes, '/tenant/plans')?.at(-1)?.route.path).toBe(
      'tenant/plans',
    );
    const layout = bundle.routes.find((route) => route.path === '/' && route.children?.length);
    expect(
      menuPaths(
        transformRoutesToMenu(layout?.children ?? bundle.routes, config.permissions ?? []) ?? [],
      ),
    ).toContain('/tenant/plans');
  });

  it('keeps the merchant build on server menus regardless of a cached platform identity', async () => {
    portal.application = 'merchant';
    const config = options();
    const bundle = await createSessionRouter(
      'frontend',
      { authenticated: true, user: { tenantId: 0 } },
      config,
    );
    routers.push(bundle.router);
    expect(config.fetchMenuListAsync).toHaveBeenCalledOnce();
    expect(matchRoutes(bundle.router.routes, '/tenant/plans')?.at(-1)?.route.path).toBe('*');
  });

  it('rejects missing server menu loading instead of falling back to local routes', () => {
    const config = options();
    delete config.fetchMenuListAsync;
    expect(() =>
      createSessionRouter('frontend', { authenticated: true, user: { tenantId: 16 } }, config),
    ).toThrow('menu loader');
  });
});
