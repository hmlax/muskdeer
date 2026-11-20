import { createElement, type ReactNode } from 'react';
import { matchRoutes, Outlet } from 'react-router-dom';
import { expect, it, vi } from 'vitest';
import { staticRoutes } from './config/static';
import { createSessionRouter } from './create-session-router';
import { transformRoutesToMenu } from '@/core/router/utils/menu';

vi.mock('@/utils', async () => import('@/utils/tree'));
vi.mock('@/components/common/Loading', () => ({ default: () => null }));
vi.mock('@/layouts/MainLayout', () => ({ default: Outlet }));
vi.mock('@/router/guards', () => ({
  AuthGuard: ({ children }: { children: ReactNode }) => children,
}));

it.each([0, 35])(
  'keeps authenticated profile reachable without adding them to merchant menus (%i)',
  async (tenantId) => {
    const result = await createSessionRouter(
      'frontend',
      { authenticated: true, user: { tenantId } },
      {
        routes: [...staticRoutes, { path: '*', element: createElement('div') }],
        permissions: ['sys:tenant_manager'],
        fetchMenuListAsync: async () => [],
        layoutMap: { BasicLayout: Outlet },
        pageMap: {},
      },
    );
    try {
      expect(matchRoutes(result.router.routes, '/opm/profile?tab=5')?.at(-1)?.route.index).toBe(
        true,
      );
      const menus = JSON.stringify(transformRoutesToMenu(result.routes, ['sys:tenant_manager']));
      expect(menus).not.toContain('/opm/profile');
    } finally {
      result.router.dispose();
    }
  },
);
