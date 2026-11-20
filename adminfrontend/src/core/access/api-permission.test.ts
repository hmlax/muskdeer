import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { canAccessApi } from './api-permission';

let useUserStore: typeof import('@/stores/user').useUserStore;

beforeEach(async () => {
  const values = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  });
  vi.resetModules();
  ({ useUserStore } = await import('@/stores/user'));
});

afterEach(() => vi.unstubAllGlobals());

describe('HTTP operation grants', () => {
  it('allows reads without exposing writes for a custom permission point', () => {
    const grants = [{ path: '/admin/v1/users', method: 'GET' }];
    expect(canAccessApi(grants, 'GET', '/admin/v1/users')).toBe(true);
    expect(canAccessApi(grants, 'POST', '/admin/v1/users')).toBe(false);
    expect(canAccessApi(grants, 'GET', '/admin/v1/roles')).toBe(false);
  });

  it('supports the verified platform administrator grant', () => {
    expect(canAccessApi([{ path: '*', method: '*' }], 'DELETE', '/admin/v1/users/{id}')).toBe(true);
  });

  it('denies operations before authorization facts are loaded', () => {
    expect(canAccessApi([], 'POST', '/admin/v1/users')).toBe(false);
  });

  it('clears grants on logout instead of carrying them into the next tenant', () => {
    useUserStore.getState().setApiPermissions([{ path: '*', method: '*' }]);
    useUserStore.getState().$reset();
    expect(useUserStore.getState().apiPermissions).toEqual([]);
  });

  it('clears authorization facts when the current identity changes', () => {
    useUserStore.getState().setUserInfo({ id: 1, tenantId: 0 } as BasicUserInfo);
    useUserStore.getState().setApiPermissions([{ path: '*', method: '*' }]);
    useUserStore.getState().setUserInfo({ id: 2, tenantId: 42 } as BasicUserInfo);
    expect(useUserStore.getState().apiPermissions).toEqual([]);
  });

  it('keeps the merchant identity through subsequent store updates and logout', () => {
    useUserStore.getState().setUserInfo({ id: 2, tenantId: 35 } as BasicUserInfo);
    useUserStore.getState().setAccessCodes(['sys:tenant_manager']);
    expect(useUserStore.getState().tenantId).toBe(35);
    expect(useUserStore.getState().isTenantUser()).toBe(true);
    expect(useUserStore.getState().isLoggedIn).toBe(true);
    useUserStore.getState().$reset();
    expect(useUserStore.getState().tenantId).toBeNull();
    expect(useUserStore.getState().isLoggedIn).toBe(false);
  });

  it('restores merchant identity and roles from the persisted user after reload', async () => {
    localStorage.setItem(useUserStore.persist.getOptions().name!, JSON.stringify({
      state: { userInfo: { id: 2, tenantId: 35, roles: ['tenant:manager'] } }, version: 0,
    }));
    await useUserStore.persist.rehydrate();
    expect(useUserStore.getState().isTenantUser()).toBe(true);
    expect(useUserStore.getState().tenantId).toBe(35);
    expect(useUserStore.getState().userRoles).toEqual(['tenant:manager']);
  });
});
