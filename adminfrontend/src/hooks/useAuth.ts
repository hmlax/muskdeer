/**
 * 认证相关 Hook
 * 统一封装登录/登出/注册/验证码/用户信息/权限码等功能
 * 对齐 Vue 版 useAuth composable
 */
import { useAuthStore } from '@/stores/auth';
import { type LoginRequest } from '@/api/generated/admin/v1';
import { useUserStore } from '@/stores/user';
import { fetchUserProfile, fetchGenerateCaptcha } from '@/api/hooks';
import { fetchMyPermissionCode } from '@/api/hooks/admin-portal';
import { useMemo } from 'react';

/**
 * 认证业务 Hook
 * 提供完整的认证操作入口，组合 store 状态 + API 调用
 */
export function createAuthActions() {
  const authStore = useAuthStore;

  /**
   * 获取当前用户信息
   */
  async function fetchUserInfo() {
    try {
      const profile = await fetchUserProfile();
      if (!profile?.id) throw new Error('User profile is missing its ID');
      return {
        ...profile,
        id: profile.id,
        tenantId: profile.tenantId ?? 0,
        avatar: profile.avatar ?? '',
        nickname: profile.nickname ?? '',
        realname: profile.realname ?? '',
        username: profile.username ?? '',
        description: profile.description ?? '',
        homePath: '',
        token: '',
      } satisfies UserInfo;
    } catch (error) {
      console.error('fetchUserInfo failed:', error);
      return null;
    }
  }

  /**
   * 获取当前用户权限码
   */
  async function fetchAccessCodes() {
    return await fetchMyPermissionCode();
  }

  /**
   * 获取用户权限码（角色码 + 权限码分开存储）
   * 首次获取后缓存到 store，后续直接读取
   */
  async function getUserPermissionCodes(): Promise<{ roles: string[]; codes: string[] } | false> {
    const userStore = useUserStore.getState();
    const { userInfo, userRoles, accessCodes, authorizationLoaded } = userStore;

    // 如果 store 中没有用户信息或权限码为空，先获取
    if (userInfo === null || !authorizationLoaded) {
      const [userInfoResult, accessCodeResult] = await Promise.all([
        fetchUserInfo(),
        fetchAccessCodes(),
      ]);

      if (!userInfoResult || !accessCodeResult) {
        console.warn('getUserPermissionCodes: failed to fetch user info or access codes');
        return false;
      }

      // 更新到 store：用户信息、角色码、权限码分开存储
      userStore.setUserInfo(userInfoResult);
      const roles = userInfoResult.roles ?? [];
      const codes = accessCodeResult.codes ?? [];
      userStore.setUserRoles(roles);
      userStore.setAccessCodes(codes);
      userStore.setHiddenFields(accessCodeResult.hiddenFields ?? []);
      userStore.setApiPermissions(accessCodeResult.apiPermissions ?? []);
      return { roles, codes };
    }

    // 已有缓存，直接返回
    return { roles: userRoles, codes: accessCodes };
  }

  /**
   * 获取验证码
   */
  async function getCaptcha() {
    return await fetchGenerateCaptcha();
  }

  return {
    /** 登录加载状态 */
    get loginLoading() {
      return authStore.getState().loginLoading;
    },
    /** 登录 */
    login: (
      params: LoginRequest,
      onSuccess?: () => void,
      captcha?: { id: string; value: string },
    ) => authStore.getState().login(params, onSuccess, captcha),
    /** 登出（主动，调后端接口） */
    logout: (redirect?: boolean) => authStore.getState().logout(redirect),
    /** 强制登出（被动，不调后端接口，用于 token 失效场景） */
    forceLogout: () => authStore.getState().forceLogout(),
    /** 注册 */
    /** 获取验证码 */
    getCaptcha,
    /** 获取用户信息 */
    fetchUserInfo,
    /** 获取权限码 */
    fetchAccessCodes,
    /** 获取角色码和权限码（分开存储） */
    getUserPermissionCodes,
  };
}

/** Reactive component facade; non-component code uses createAuthActions. */
export function useAuth() {
  const loginLoading = useAuthStore((state) => state.loginLoading);
  const actions = useMemo(() => createAuthActions(), []);
  return { ...actions, loginLoading };
}
