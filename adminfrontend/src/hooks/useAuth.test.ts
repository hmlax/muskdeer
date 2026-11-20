import { afterEach, beforeEach, expect, it, vi } from 'vitest';

const mocks = vi.hoisted(() => ({ profile: vi.fn(), grants: vi.fn() }));
vi.mock('@/stores/auth', () => ({ useAuthStore: { getState: () => ({}) } }));
vi.mock('@/api/hooks', () => ({ fetchUserProfile: mocks.profile, fetchGenerateCaptcha: vi.fn() }));
vi.mock('@/api/hooks/admin-portal', () => ({ fetchMyPermissionCode: mocks.grants }));

beforeEach(() => {
  vi.resetModules();
  const values = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  });
  vi.clearAllMocks();
});
afterEach(() => vi.unstubAllGlobals());

it('reloads API capabilities after a browser refresh even when persisted roles are present', async () => {
  const { useUserStore } = await import('@/stores/user');
  localStorage.setItem(
    useUserStore.persist.getOptions().name!,
    JSON.stringify({
      state: { userInfo: { id: 2, tenantId: 35, roles: ['tenant:manager'] } },
      version: 0,
    }),
  );
  await useUserStore.persist.rehydrate();
  mocks.profile.mockResolvedValue({ id: 2, tenantId: 35, roles: ['tenant:manager'] });
  const apiPermissions = [{ method: 'POST', path: '/admin/v1/users' }];
  mocks.grants.mockResolvedValue({ codes: ['sys:tenant_manager'], apiPermissions });
  const { createAuthActions } = await import('./useAuth');
  await createAuthActions().getUserPermissionCodes();
  expect(mocks.grants).toHaveBeenCalledOnce();
  expect(useUserStore.getState().apiPermissions).toEqual(apiPermissions);
  expect(useUserStore.getState().isTenantUser()).toBe(true);
  await createAuthActions().getUserPermissionCodes();
  expect(mocks.grants).toHaveBeenCalledOnce();
});
