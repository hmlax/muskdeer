import { type ComponentType, createElement, type ReactNode, Suspense } from 'react';
import { Navigate } from 'react-router-dom';

import { mapTree } from '@/utils';
import Loading from '@/components/common/Loading';
import type {
  AppRouteObject,
  BackendRoute,
  ComponentRecord,
  GenerateRoutesOptions,
} from '@/core/router/types'; // 全局加载组件
import { hasPermission } from './generate-routes-frontend';

const LEGACY_TENANT_MANAGER_AUTHORITY = 'sys:tenant_manager';

/**
 * 动态生成路由 - 后端方式（React 版本）
 * @param options - 配置选项
 * @returns 转换后的路由树（AppRouteObject[]），可直接传入 createBrowserRouter
 */
export async function generateRoutesByBackend(
  options: GenerateRoutesOptions,
): Promise<AppRouteObject[]> {
  const {
    fetchMenuListAsync,
    layoutMap = {},
    pageMap = {},
    permissions = [],
    forbiddenElement,
  } = options;

  try {
    // 1. 从后端获取菜单路由（组件路径为字符串）
    const menuRoutes = await fetchMenuListAsync?.();
    if (!menuRoutes?.length) {
      return [];
    }

    // 2. 预处理 pageMap：标准化路径键名（支持多种写法）
    const normalizedPageMap: ComponentRecord = {};
    for (const [key, value] of Object.entries(pageMap)) {
      const normalizedKey = normalizeViewPath(key);
      normalizedPageMap[normalizedKey] = value;
      // 兼容：如果传入的是带后缀的，也注册不带后缀的映射
      if (normalizedKey.endsWith('.tsx')) {
        normalizedPageMap[normalizedKey.replace(/\.tsx$/, '')] = value;
      }
    }

    // 3. 先过滤权限，再归位路径并选可访问的目录默认页
    const visibleRoutes = filterBackendPermissions(
      menuRoutes,
      permissions,
      layoutMap,
      normalizedPageMap,
    );
    rebaseBackendTree(visibleRoutes);
    return convertRoutes(visibleRoutes, layoutMap, normalizedPageMap, forbiddenElement);
  } catch (error) {
    console.error('Failed to generate routes from backend:', error);
    return [];
  }
}

function filterBackendPermissions(
  nodes: BackendRoute[],
  permissions: string[],
  layoutMap: ComponentRecord,
  pageMap: ComponentRecord,
): BackendRoute[] {
  const result: BackendRoute[] = [];
  for (const node of nodes) {
    const meta = node.meta ? { ...node.meta } : undefined;
    // The server has already granted this menu to the current role. Legacy
    // tenant-manager labels must not reject custom tenant roles a second
    // time; platform-only nodes retain their own authority requirements.
    if (meta?.authority?.includes(LEGACY_TENANT_MANAGER_AUTHORITY)) {
      meta.authority = undefined;
    }
    const grantedNode = { ...node, meta };
    if (!hasPermission(grantedNode, permissions) && !meta?.menuVisibleWithForbidden) continue;
    const children = node.children
      ? filterBackendPermissions(node.children, permissions, layoutMap, pageMap)
      : undefined;
    const catalog = node.component === 'BasicLayout' || !node.component;
    // Installed databases can retain menu rows for pages removed from the framework.
    if (
      node.component &&
      !layoutMap[node.component] &&
      !meta?.link &&
      !meta?.iframeSrc &&
      !resolveComponent(node.component, layoutMap, pageMap)
    )
      continue;

    if (catalog && !children?.length && !node.redirect && !meta?.link && !meta?.iframeSrc) continue;
    if (catalog && node.children?.length && !children?.length) continue;
    result.push({
      ...grantedNode,
      children,
    });
  }
  return result;
}

function resolveComponent(
  component: string,
  layouts: ComponentRecord,
  pages: ComponentRecord,
): ComponentType | undefined {
  if (layouts[component]) return layouts[component];
  const normalized = normalizeViewPath(component);
  return (
    pages[normalized] ??
    pages[`${normalized}.tsx`] ??
    pages[`${normalized}/index`] ??
    pages[`${normalized}/index.tsx`]
  );
}

/**
 * 递归转换后端路由为 React 路由
 */
function convertRoutes(
  routes: BackendRoute[],
  layoutMap: ComponentRecord,
  pageMap: ComponentRecord,
  forbiddenElement?: ReactNode,
): AppRouteObject[] {
  // 注意：不手动递归 children，由 mapTree 自动处理递归
  // children 必须保留在返回对象中，mapTree 才能自动递归
  return mapTree(routes, (node) => {
    const { component: componentPath, name, ...rest } = node;

    if (!name && !node.index) {
      console.error('Route name is required', node);
    }

    const route: AppRouteObject = {
      ...rest,
      name,
    };

    const forbidden = !!node.meta?.authority?.length && node.meta.menuVisibleWithForbidden;
    if (forbidden && forbiddenElement) {
      route.element = forbiddenElement;
    } else if (componentPath) {
      const Component = resolveComponent(componentPath, layoutMap, pageMap);

      // 3. 渲染元素：必须用 Suspense 包裹 lazy 组件
      if (Component) {
        route.element = createElement(
          Suspense,
          { fallback: createElement(Loading) },
          createElement(Component),
        );
      } else {
        // 开发环境警告，生产环境降级处理
        if (import.meta.env.DEV) {
          console.warn(`Component not found for path: ${componentPath}`);
        }
        route.element = createElement('div', null, `404 - Component "${componentPath}" not found`);
      }
    }

    // 处理 redirect：将后端返回的 redirect 转换为 React Router 可识别的导航
    // 1. 有 redirect + 有 children → 注入 index 子路由进行重定向
    // 2. 有 redirect + 无 component → 纯重定向路由，element 设为 <Navigate>
    if (rest.redirect && !forbidden) {
      if (rest.children?.length) {
        // 在 children 最前面注入 index 路由（匹配父路径本身）
        const indexRoute: AppRouteObject = {
          index: true,
          element: createElement(Navigate, { to: rest.redirect, replace: true }),
          meta: { hideInMenu: true, hideInTab: true },
        };
        route.children = [indexRoute, ...rest.children];
      } else if (!componentPath) {
        // 纯重定向路由（无组件、无子路由）
        route.element = createElement(Navigate, { to: rest.redirect, replace: true });
      }
    }

    return route;
  });
}

/**
 * 归位后端菜单树中的嵌套绝对子路径，并为缺省目录组注入重定向。
 *
 * 后端菜单树是 vue-router 风格：目录组内子项常带绝对路径（/dashboard →
 * /analytics、/profile → /profile）。vue-router 允许嵌套绝对子路径，而
 * react-router 禁止（除非以父路径全路径为前缀），createBrowserRouter 会直接
 * 抛错卡死路由创建。这里把绝对子路径归位为父目录组内的相对路径：
 * - 前缀与父路径匹配者剥掉父前缀（/profile → 空路径 index）；
 * - 其余剥掉开头斜杠（/analytics → analytics），URL 随父路径延伸为
 *   /dashboard/analytics，与前端模式静态路由的 URL 空间保持一致。
 */
function rebaseBackendTree(nodes: BackendRoute[], parentPath = ''): void {
  for (const node of nodes) {
    if (parentPath && node.path?.startsWith('/')) {
      node.path =
        node.path === parentPath || node.path.startsWith(`${parentPath}/`)
          ? node.path.slice(parentPath.length).replace(/^\/+/, '')
          : node.path.replace(/^\/+/, '');
    }
    const fullPath = node.path ? resolveRoutePath(parentPath, node.path) : parentPath;

    if (node.children?.length) {
      rebaseBackendTree(node.children, fullPath);
    }

    if (node.redirect && node.children?.length) {
      const target = resolveRoutePath(fullPath, node.redirect);
      if (target === fullPath || !containsRoutePath(node.children, fullPath, target)) {
        node.redirect = undefined;
      }
    }

    // 目录组缺省重定向：convertRoutes 会将其转成 index <Navigate>，否则
    // 直访目录组路径（/dashboard）只渲染出空布局。默认页排除隐藏项和
    // 需要参数的详情页，重定向目标等于自身时不注入。
    const firstChild = [...(node.children ?? [])]
      .filter((child) => child.path && !child.meta?.hideInMenu && !child.path.includes(':'))
      .sort(
        (a, b) =>
          (a.meta?.order ?? Number.MAX_SAFE_INTEGER) - (b.meta?.order ?? Number.MAX_SAFE_INTEGER),
      )[0];
    if (node.children?.length && !node.redirect && firstChild?.path) {
      const target = resolveRoutePath(fullPath, firstChild.path);
      if (target !== fullPath) {
        node.redirect = target;
      }
    }
  }
}

function containsRoutePath(nodes: BackendRoute[], parentPath: string, target: string): boolean {
  return nodes.some((node) => {
    const path = resolveRoutePath(parentPath, node.path ?? '');
    return (
      path === target || (!!node.children?.length && containsRoutePath(node.children, path, target))
    );
  });
}

function resolveRoutePath(parentPath: string, childPath: string): string {
  if (!childPath) return parentPath;
  if (childPath.startsWith('/')) return childPath;
  return `${parentPath.replace(/\/+$/, '')}/${childPath}`;
}

/**
 * 标准化视图路径（适配 React 目录结构）
 * @param path - 后端返回的组件路径，如 "./views/system/user" 或 "/views/dashboard"
 */
export function normalizeViewPath(path: string): string {
  // 1. 去除相对路径前缀
  const normalized = path.replace(/^(\.\/|\.\.\/)+/, '');

  // 2. 确保以 / 开头
  const withSlash = normalized.startsWith('/') ? normalized : `/${normalized}`;

  // 3. 去除目录前缀（适配 React 项目结构）
  // 支持多种常见目录：/views, /pages, /src/pages 等；后端组件路径带 app/ 段
  // （如 app/opm/user/list/index.vue），而 React pageMap 键不含 app/，一并剥掉；
  // 后端种子用下划线目录（vue-element 端约定，如 internal_message），React
  // 页面目录统一横线，归一化时转换
  return withSlash
    .replace(/^\/(src\/)?(views|pages)\//i, '/') // 去除 /views/ 或 /pages/
    .replace(/^\/app\//, '/') // 去除 /app/ 段
    .replace(/\.(vue|tsx|jsx|ts)$/i, '') // 去除组件文件扩展名（后端下发的是 vue 端路径 xxx/index.vue）
    .replace(/_/g, '-') // 下划线 → 横线（React 页面目录约定）
    .replace(/\/+$/, ''); // 去除末尾斜杠
}
