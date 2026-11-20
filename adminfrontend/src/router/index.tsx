import { lazy, useState, useEffect } from 'react';
import { pageModules } from '@app/pages';
import { RouterProvider } from 'react-router-dom';

import { useAuthStore, useUserStore } from '@/stores';
import { createAuthActions } from '@/hooks/useAuth';
import { getAccessStatic } from '@/core/access';
import { fetchAllDictEntries } from '@/hooks/useDictCache';
import { usePreferencesStore } from '@/core/preferences/store';
import { apiClient } from '@/api/client';
import { useI18n } from '@/core/i18n';

import { Forbidden } from '@/pages/core/error';
import { AccessibleRoutesContext } from '@/core/router';
import type { AppRouteObject, ComponentRecordType } from '@/core/router';
import MainLayout from '@/layouts/MainLayout';
import { AuthGuard } from '@/router/guards';

import { errorRoutes } from './config/error-routes';
import Loading from '@/components/common/Loading';
import { authRoutes } from './config/auth';
import { staticRoutes } from './config/static';
import { createSessionRouter } from './create-session-router';

// 布局组件映射（后端 component 字段 → React 组件）
// 后端模式：BasicLayout 需要包裹 AuthGuard（前端模式已在 staticRoutes 中包裹）
const AuthenticatedLayout = () => (
  <AuthGuard>
    <MainLayout />
  </AuthGuard>
);

const layoutMap: ComponentRecordType = {
  BasicLayout: AuthenticatedLayout,
};

// Only the current application's pages enter the bundle; load them on demand.
const pageMap: ComponentRecordType = {};
for (const [globPath, loadPage] of Object.entries(pageModules)) {
  const Component = lazy(loadPage);
  // globPath: "../pages/app/dashboard/index.tsx"
  // 提取 app/ 之后的路径部分
  const appMatch = globPath.match(/(?:pages|views)\/app\/(.+)/);
  if (!appMatch) continue;

  const relativePath = appMatch[1] // "dashboard/index.tsx"
    .replace(/\.tsx$/, '') // "dashboard/index"
    .replace(/\/index$/, ''); // "dashboard"

  // 生成与 normalizeViewPath 一致的键
  const normalizedKey = `/${relativePath}`; // "/dashboard"
  pageMap[normalizedKey] = Component;
  pageMap[`${normalizedKey}/index`] = Component; // "/dashboard/index"
  pageMap[`${normalizedKey}.tsx`] = Component; // "/dashboard.tsx"
  pageMap[`${normalizedKey}/index.tsx`] = Component; // "/dashboard/index.tsx"
}

// 业务模块路由在 ./business-routes.ts 中统一提取（allRoutes 组装与菜单同步共用）
import { businessRoutes } from './business-routes';
export { businessRoutes };

// 合并路由：将业务模块路由合并到主布局容器的 children 中
export const allRoutes: AppRouteObject[] = [
  ...staticRoutes.map((route) => {
    // 找到主布局容器路由（path 为 '/' 且包含 children）
    if (route.path === '/' && route.children) {
      return {
        ...route,
        children: [...route.children, ...businessRoutes],
      };
    }
    return route;
  }),
  ...authRoutes, // 认证路由（独立，不受 AuthGuard 保护）
  ...errorRoutes, // 错误路由放在最后
];

export const AppRouter = () => {
  const [routerBundle, setRouterBundle] = useState<{
    router: unknown;
    routes: AppRouteObject[];
  } | null>(null);
  const [loading, setLoading] = useState(true);

  const { accessToken } = useAuthStore();
  const accessMode = usePreferencesStore((s) => s.preferences.app.accessMode);
  const { t } = useI18n('common');

  const isAuthenticated = !!accessToken;

  useEffect(() => {
    let stale = false;

    const initRouter = async () => {
      setLoading(true);

      try {
        // ========== 已认证时的初始化流程 ==========
        // 对齐 Vue 版 setupAccessGuard：权限码获取 + 字典预加载
        if (isAuthenticated) {
          try {
            const auth = createAuthActions();

            // 获取用户权限码（角色 + 权限码，首次会调 API）；失败=认证失效走登出
            await auth.getUserPermissionCodes();
          } catch (authErr) {
            // 认证失败（token 过期/无效）：forceLogout 已在拦截器中被调用
            // 清除 userStore 防止脏数据
            console.warn('Auth initialization failed, will redirect to login:', authErr);
            // forceLogout 已在拦截器中处理，此处清除 userStore
            const { useUserStore } = await import('@/stores');
            useUserStore.getState().$reset();

            if (stale) return; // 已过期，不继续创建路由
          }

          // 字典预载失败不阻断进入应用：页面退化为未翻译字典码。瞬时接口抖动
          // 不应触发登出（此前与权限获取同 try，字典一抖就把用户踹回登录页）
          try {
            await fetchAllDictEntries();
          } catch (dictErr) {
            console.warn('[Router] 字典预加载失败，页面将以未翻译字典码降级渲染:', dictErr);
          }
        }

        // await 之后，通过 useAccess 获取最新合并权限（角色码 + 权限码）
        const freshPermissions = getAccessStatic().getAllPermissions();

        // 无论认证是否成功，都生成路由（未认证时 permissions 为空，AuthGuard 会拦截）
        const { router: appRouter, routes: generatedRoutes } = await createSessionRouter(
          accessMode,
          {
            authenticated: isAuthenticated,
            user: useUserStore.getState().userInfo,
          },
          {
            routes: allRoutes,
            permissions: freshPermissions,
            forbiddenElement: <Forbidden />,
            fetchMenuListAsync: async () => {
              const data = await apiClient.adminPortalService.GetNavigation({});
              return data.items ?? [];
            },
            layoutMap,
            pageMap,
            autoInjectRedirect: true,
            autoSort: true,
          },
        );

        if (!stale) {
          setRouterBundle({ router: appRouter, routes: generatedRoutes });
        }
      } catch (err) {
        console.error('Router init failed:', err);
      } finally {
        if (!stale) {
          setLoading(false);
        }
      }
    };

    initRouter();

    // cleanup：当 effect 重新触发时（isAuthenticated 变化），取消上一次 initRouter
    return () => {
      stale = true;
    };
  }, [isAuthenticated, accessMode]);

  if (loading || !routerBundle)
    return (
      <Loading fullScreen text={t('loading.initializing')} subText={t('loading.loadingRouter')} />
    );

  // 挂载路由树经 Context 下发：侧栏镜像实际挂载路由（后端模式=后端下发，
  // 前端模式=权限过滤后），避免后端模式侧栏回退静态全量表造成越权观感
  return (
    <AccessibleRoutesContext.Provider value={routerBundle.routes}>
      <RouterProvider router={routerBundle.router} />
    </AccessibleRoutesContext.Provider>
  );
};
