import { Navigate } from 'react-router-dom';

import { createLazyRoute, type AppRouteObject } from '@/core/router';
import { AuthGuard } from '@/router/guards';
import MainLayout from '@/layouts/MainLayout';

/**
 * 静态基础路由配置
 */
export const staticRoutes: AppRouteObject[] = [
  // 普通业务路由的主布局容器
  // 直接使用 '/' 作为路径，业务路由会作为其子路由
  {
    path: '/',
    element: (
      <AuthGuard>
        <MainLayout />
      </AuthGuard>
    ),
    meta: {
      hideInMenu: true,
      hideInTab: true,
    },
    children: [
      // 根路径重定向到分析页（登录落地页，与 ele/vben 的 DEFAULT_HOME_PATH=/analytics 对齐）
      {
        path: '/',
        index: true,
        element: <Navigate to="/dashboard/analytics" replace />,
        meta: { title: 'routes:home', hideInMenu: true, hideInTab: true },
      },
    ],
  },
  // Self-service profile remains available in both routing modes. Its API
  // always uses the authenticated user's identity and tenant access checks.
  {
    path: '/opm/profile',
    element: (
      <AuthGuard>
        <MainLayout />
      </AuthGuard>
    ),
    meta: { hideInMenu: true, hideInTab: true },
    children: [
      {
        name: 'user-profile',
        index: true,
        element: createLazyRoute(() => import('@/pages/app/opm/user/profile')),
        meta: { title: 'routes:user-profile', hideInMenu: true, hideInTab: false },
      },
    ],
  },
  {
    path: '/internal-message/inbox',
    element: (
      <AuthGuard>
        <MainLayout />
      </AuthGuard>
    ),
    meta: { hideInMenu: true, hideInTab: true },
    children: [],
  },
];
