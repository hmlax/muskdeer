import { createElement, isValidElement } from 'react';
import { matchRoutes, Outlet } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createSessionRouter } from './create-session-router';
import { transformRoutesToMenu } from '@/core/router/utils/menu';
import type { BackendRoute } from '@/core/router/types';

vi.mock('@/utils', async () => import('@/utils/tree'));
vi.mock('@/components/common/Loading', () => ({ default: () => null }));

const routers: Array<{ dispose: () => void }> = [];
afterEach(() => routers.splice(0).forEach((router) => router.dispose()));

type MenuItem = { path?: string; name?: string; children?: MenuItem[] };
function flatten(items: MenuItem[]): MenuItem[] {
  return items.flatMap((item) => [item, ...flatten(item.children ?? [])]);
}

function menuTree(): BackendRoute[] {
  const tenant = ['sys:tenant_manager'];
  const platform = ['sys:platform_admin'];
  return [
    {
      name: 'Opm',
      path: '/opm',
      component: 'BasicLayout',
      redirect: '/opm/users',
      meta: { title: '组织人员', authority: tenant },
      children: [
        {
          name: 'Users',
          path: 'users',
          component: 'app/opm/user/list/index.vue',
          meta: { title: '用户管理', authority: tenant },
        },
        {
          name: 'UserDetail',
          path: 'users/detail/:id',
          component: 'app/opm/user/detail/index.vue',
          meta: { title: '用户详情', hideInMenu: true, authority: tenant },
        },
      ],
    },
    {
      name: 'Permission',
      path: '/permission',
      component: 'BasicLayout',
      redirect: '/permission/codes',
      meta: { title: '权限管理', authority: tenant },
      children: [
        {
          name: 'Codes',
          path: 'codes',
          component: 'app/permission/permission/index.vue',
          meta: { title: '权限点', authority: platform, order: 1 },
        },
        {
          name: 'Roles',
          path: 'roles',
          component: 'app/permission/role/index.vue',
          meta: { title: '角色管理', authority: tenant, order: 4 },
        },
      ],
    },
    {
      name: 'Messages',
      path: '/internal-message',
      component: 'BasicLayout',
      redirect: '/internal-message/messages',
      meta: { title: '站内消息', authority: tenant },
      children: [
        {
          name: 'MessageList',
          path: 'messages',
          component: 'app/internal_message/message/index.vue',
          meta: { title: '消息管理', authority: tenant },
        },
      ],
    },
    {
      name: 'System',
      path: '/system',
      component: 'BasicLayout',
      redirect: '/system/dict',
      meta: { title: '系统管理', authority: tenant },
      children: [
        {
          name: 'Dict',
          path: 'dict',
          component: 'app/system/dict/index.vue',
          meta: { title: '字典管理', authority: platform, order: 1 },
        },
        {
          name: 'Files',
          path: 'files',
          component: 'app/system/file/index.vue',
          meta: { title: '文件管理', authority: tenant, order: 2 },
        },
        {
          name: 'Tasks',
          path: 'tasks',
          component: 'app/system/task/index.vue',
          meta: { title: '任务管理', authority: tenant, order: 3 },
        },
      ],
    },
  ];
}

async function bundle(tree = menuTree(), permissions = ['sys:tenant_manager']) {
  const pageMap: Record<string, () => ReturnType<typeof createElement>> = {};
  for (const root of tree) {
    for (const page of root.children ?? []) {
      const path = page
        .component!.replace(/^app\//, '')
        .replace(/_/g, '-')
        .replace(/\.vue$/, '');
      pageMap['/' + path] = () => createElement('div');
    }
  }
  const result = await createSessionRouter(
    'frontend',
    { authenticated: true, user: { tenantId: 16 } },
    {
      routes: [{ path: '*', element: createElement('div') }],
      permissions,
      layoutMap: { BasicLayout: Outlet },
      pageMap,
      fetchMenuListAsync: async () => tree,
    },
  );
  routers.push(result.router);
  return result;
}

describe('backend menus rendered for tenant users', () => {
  it('renders a server-granted custom tenant role without the legacy manager label', async () => {
    const tree = menuTree();
    const granted = [
      tree[0],
      {
        ...tree[1],
        children: [tree[1].children![0]],
      },
    ];
    const permissions = ['sys:access_backend', 'qa:users:view'];
    const result = await bundle(granted, permissions);
    expect(matchRoutes(result.router.routes, '/opm/users')?.at(-1)?.route.path).toBe('users');
    expect(matchRoutes(result.router.routes, '/permission/codes')?.at(-1)?.route.path).toBe('*');
    expect(matchRoutes(result.router.routes, '/permission/roles')?.at(-1)?.route.path).toBe('*');
    const menus = flatten(transformRoutesToMenu(result.routes, permissions) ?? []);
    expect(menus.some((item) => item.path === '/opm/users')).toBe(true);
    expect(menus.some((item) => item.path === '/permission')).toBe(false);
    const root = matchRoutes(result.router.routes, '/')?.at(-1)?.route;
    expect(isValidElement<{ to: string }>(root?.element) && root.element.props.to).toBe(
      '/opm/users',
    );
  });

  it('does not turn redirect index routes into blank menu rows', async () => {
    const result = await bundle();
    const items = flatten(transformRoutesToMenu(result.routes, ['sys:tenant_manager']) ?? []);
    expect(items.every((item) => !!item.path && !!item.name)).toBe(true);
    expect(items.filter((item) => item.path === '/opm')).toHaveLength(1);
    expect(items.filter((item) => item.path === '/permission')).toHaveLength(1);
    expect(items.filter((item) => item.path === '/internal-message')).toHaveLength(1);
  });

  it('keeps allowed system pages visible and chooses an allowed default page', async () => {
    const result = await bundle();
    const menus = transformRoutesToMenu(result.routes, ['sys:tenant_manager']) ?? [];
    const system = menus.find((item: MenuItem) => item.path === '/system');
    expect(system?.children?.map((item: MenuItem) => item.name)).toEqual(['文件管理', '任务管理']);
    const index = matchRoutes(result.router.routes, '/system')?.at(-1)?.route;
    expect(index?.index).toBe(true);
    expect(isValidElement<{ to: string }>(index?.element) && index.element.props.to).toBe(
      '/system/files',
    );
    expect(matchRoutes(result.router.routes, '/system/files')?.at(-1)?.route.path).toBe('files');
    expect(matchRoutes(result.router.routes, '/system/tasks')?.at(-1)?.route.path).toBe('tasks');
  });

  it('does not mount hidden platform pages or redirect to an unavailable permission page', async () => {
    const result = await bundle();
    expect(matchRoutes(result.router.routes, '/system/dict')?.at(-1)?.route.path).toBe('*');
    expect(matchRoutes(result.router.routes, '/permission/codes')?.at(-1)?.route.path).toBe('*');
    const index = matchRoutes(result.router.routes, '/permission')?.at(-1)?.route;
    expect(isValidElement<{ to: string }>(index?.element) && index.element.props.to).toBe(
      '/permission/roles',
    );
    expect(matchRoutes(result.router.routes, '/opm/users/detail/7')?.at(-1)?.route.path).toBe(
      'users/detail/:id',
    );
    const menus = flatten(transformRoutesToMenu(result.routes, ['sys:tenant_manager']) ?? []);
    expect(menus.some((item) => item.path?.includes(':id'))).toBe(false);
  });

  it('removes catalogs without allowed children and preserves the source menu tree', async () => {
    const tree = menuTree();
    tree.push({
      name: 'Audit',
      path: '/log',
      component: 'BasicLayout',
      meta: { title: '日志审计', authority: ['sys:tenant_manager'] },
      children: [
        {
          name: 'LoginAudit',
          path: 'login',
          component: 'app/log/login/index.vue',
          meta: { title: '登录日志', authority: ['sys:platform_admin'] },
        },
      ],
    });
    const original = structuredClone(tree);
    const result = await bundle(tree);
    const menus = flatten(transformRoutesToMenu(result.routes, ['sys:tenant_manager']) ?? []);
    expect(menus.some((item) => item.path === '/log')).toBe(false);
    expect(matchRoutes(result.router.routes, '/log/login')?.at(-1)?.route.path).toBe('*');
    expect(tree).toEqual(original);
  });
  it('omits unavailable installed business pages and their empty catalogs', async () => {
    const tree: BackendRoute[] = [
      {
        name: 'Removed',
        path: '/removed',
        component: 'BasicLayout',
        children: [{ name: 'OldPage', path: 'old', component: 'retired-business/index.vue' }],
      },
      { name: 'Users', path: '/users', component: 'app/opm/user/list/index.vue' },
    ];
    const original = structuredClone(tree);
    const result = await createSessionRouter(
      'backend',
      { authenticated: true, user: { tenantId: 16 } },
      {
        routes: [{ path: '*', element: createElement('div') }],
        permissions: [],
        layoutMap: { BasicLayout: Outlet },
        pageMap: { '/opm/user/list/index': () => createElement('div') },
        fetchMenuListAsync: async () => tree,
      },
    );
    routers.push(result.router);
    expect(matchRoutes(result.router.routes, '/removed/old')?.at(-1)?.route.path).toBe('*');
    expect(matchRoutes(result.router.routes, '/users')?.at(-1)?.route.path).toBe('/users');
    expect(
      flatten(transformRoutesToMenu(result.routes, []) ?? []).some(
        (item) => item.path === '/removed',
      ),
    ).toBe(false);
    expect(tree).toEqual(original);
  });
});
