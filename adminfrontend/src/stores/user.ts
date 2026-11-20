import { applicationStorageKey } from '@/config/application';
import {create} from 'zustand';
import {persist} from 'zustand/middleware';
import type {ApiGrant} from '@/api/generated/admin/v1';

// ========== 状态定义 ==========
export interface UserState {
    // 核心状态
    userInfo: BasicUserInfo | null;
    userRoles: string[];         // 角色码（来自 userInfo.roles）
    accessCodes: string[];       // 权限码（来自 GetMyPermissionCode）
    hiddenFields: string[];      // 字段权限隐藏字段集（"资源.字段" 串，来自 GetMyPermissionCode）
    apiPermissions: ApiGrant[];
    authorizationLoaded: boolean;

    // 计算属性（函数形式，React 中不需要 computed）
    tenantId: number | null;
    isLoggedIn: boolean;

    // 动作
    setUserInfo: (info: BasicUserInfo | null) => void;
    setUserRoles: (roles: string[]) => void;
    setAccessCodes: (codes: string[]) => void;
    setHiddenFields: (fields: string[]) => void;
    setApiPermissions: (grants: ApiGrant[]) => void;
    isTenantUser: () => boolean;
    $reset: () => void;
}

// ========== Store 实现 ==========
export const useUserStore = create<UserState>()(
    persist(
        (set, get) => ({
            // 初始状态
            userInfo: null,
            userRoles: [],
            accessCodes: [],
            hiddenFields: [],
            apiPermissions: [],
            authorizationLoaded: false,

            // 计算属性（函数形式，每次调用时计算）
            tenantId: null,
            isLoggedIn: false,

            // 设置用户信息（同时更新角色）
            setUserInfo: (info) => {
                set((state) => ({
                    userInfo: info,
                    tenantId: info?.tenantId ?? null,
                    isLoggedIn: info !== null,
                    userRoles: info?.roles ?? [],
                    ...(!info || state.userInfo?.id !== info.id || state.userInfo?.tenantId !== info.tenantId
                        ? {accessCodes: [], hiddenFields: [], apiPermissions: [], authorizationLoaded: false}
                        : {}),
                }));
            },

            // 单独设置角色
            setUserRoles: (roles) => {
                set({userRoles: roles});
            },

            // 单独设置权限码
            setAccessCodes: (codes) => {
                set({accessCodes: codes});
            },

            // 单独设置字段权限隐藏字段集
            setHiddenFields: (fields) => {
                set({hiddenFields: fields});
            },
            setApiPermissions: (grants) => {
                set({apiPermissions: grants, authorizationLoaded: true});
            },

            // 判断是否为租户用户
            isTenantUser: () => {
                const {tenantId} = get();
                return tenantId !== null && tenantId !== undefined && tenantId > 0;
            },

            // 重置状态
            $reset: () => {
                set({
                    userInfo: null,
                    tenantId: null,
                    isLoggedIn: false,
                    userRoles: [],
                    accessCodes: [],
                    hiddenFields: [],
                    apiPermissions: [],
                    authorizationLoaded: false,
                });
            },
        }),
        {
            name: applicationStorageKey('user'), // localStorage key
            merge: (persisted, current) => {
                const info = (persisted as Partial<UserState> | undefined)?.userInfo ?? null;
                return {
                    ...current,
                    userInfo: info,
                    userRoles: info?.roles ?? [],
                    tenantId: info?.tenantId ?? null,
                    isLoggedIn: info !== null,
                    accessCodes: [],
                    hiddenFields: [],
                    apiPermissions: [],
                    authorizationLoaded: false,
                };
            },
            // ✅ 可选：只持久化用户信息（角色可从用户信息派生）
            partialize: (state) => ({
                userInfo: state.userInfo,
                // userRoles 可从 userInfo?.roles 派生，不持久化避免不一致
            }),
        }
    )
);
