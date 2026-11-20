import { describe, expect, it } from 'vitest';
import { pageModules as platformPages } from '../../platform-admin/pages';
import { pageModules as merchantPages } from '../../merchant-admin/pages';
import { businessRoutes as merchantRoutes } from '../../merchant-admin/routes';

describe('application page boundaries', () => {
  it.each([
    'tenant/tenant/index.tsx',
    'muskdeer/templates/index.tsx',
    'muskdeer/instruments/index.tsx',
    'muskdeer/gateways/index.tsx',
    'permission/menu/index.tsx',
    'permission/api/index.tsx',
    'permission/permission/index.tsx',
    'system/config/index.tsx',
  ])('includes %s only in the platform build', (page) => {
    const key = `../src/pages/app/${page}`;
    expect(platformPages).toHaveProperty(key);
    expect(Object.keys(merchantPages)).not.toContain(key);
  });

  it.each([
    'dashboard/analytics/index.tsx',
    'permission/role/index.tsx',
    'opm/user/list/index.tsx',
    'opm/user/profile/index.tsx',
    'system/file/index.tsx',
  ])('retains %s for authorized merchant menus', (page) => {
    expect(Object.keys(merchantPages)).toContain(`../src/pages/app/${page}`);
  });

  it.each(['instances', 'accounts', 'orders', 'cancellations', 'snapshot'])(
    'keeps Muskdeer %s only in the merchant bundle',
    (page) => {
      const key = `../src/pages/app/muskdeer/${page}/index.tsx`;
      expect(Object.keys(merchantPages)).toContain(key);
      expect(Object.keys(platformPages)).not.toContain(key);
    },
  );

  it('does not install static merchant business menus before server authorization', () => {
    expect(merchantRoutes).toEqual([]);
  });

  it.each(['overview', 'synthetic'])(
    'does not ship the retired %s page in either portal',
    (page) => {
      const key = `../src/pages/app/muskdeer/${page}/index.tsx`;
      expect(Object.keys(merchantPages)).not.toContain(key);
      expect(Object.keys(platformPages)).not.toContain(key);
    },
  );
});
