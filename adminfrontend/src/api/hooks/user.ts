import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type User,
  type ListUserResponse,
  type GetUserRequest,
  type UserExistsRequest,
  type EditUserPasswordRequest,
  type UserExistsResponse,
} from '@/api/generated/admin/v1';
import { makeUpdateMask, type PaginationQuery } from '@/core/transport/rest';
import { apiClient } from '@/api/client';
import { queryClient } from '@/core';
import { encryptPassword } from '@/utils';

/**
 * 获取用户列表（剥离不需要的分页参数）
 */
function listUsers(query: PaginationQuery) {
  const params = query.toRawParams();
  return apiClient.userService.List({
    ...params,
    sorting: undefined,
    offset: undefined,
    limit: undefined,
    token: undefined,
    filter: undefined,
    filterExpr: undefined,
  });
}

// ==============================
// 获取用户列表
// ==============================
export function useListUsers(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListUserResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listUsers', query],
    queryFn: () => listUsers(query),
    ...options,
  });
}

// ==============================================
// 获取用户列表 【给 Store / 外部调用】不带 Hook 的方法
// ==============================================
export async function fetchListUsers(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listUsers', params],
    queryFn: () => listUsers(params),
    retry: 0,
  });
}

// ==============================
// 获取单个用户
// ==============================
export function useGetUser(
  req: GetUserRequest,
  options?: Omit<UseQueryOptions<User, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getUser', req],
    queryFn: () => apiClient.userService.Get(req),
    ...options,
  });
}

// ==============================================
// 获取单个用户 【给 Store / 外部调用】不带 Hook 的方法
// ==============================================
export async function fetchUser(params: GetUserRequest) {
  return queryClient.fetchQuery({
    queryKey: ['getUser', params],
    queryFn: () => apiClient.userService.Get(params),
    retry: 0,
  });
}

// ==============================
// 创建用户
// ==============================
export function useCreateUser(
  options?: UseMutationOptions<Record<string, never>, Error, { data: User; password?: string }>,
) {
  return useMutation({
    mutationFn: ({ data, password }) => apiClient.userService.Create({ data, password }),
    ...options,
  });
}

// ==============================
// 删除用户
// ==============================
export function useDeleteUser(options?: UseMutationOptions<Record<string, never>, Error, number>) {
  return useMutation({
    mutationFn: (id) => apiClient.userService.Delete({ id }),
    ...options,
  });
}

// ==============================
// 更新用户
// ==============================
export function useUpdateUser(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.userService.Update>[0]['data']> & {
        password?: string;
      };
    }
  >,
) {
  return useMutation({
    // proto UpdateUserRequest.password 是顶层字段且要求 AES 密文：
    // 留在 data/updateMask 里后端读不到，会静默跳过重置密码（200 但密码未变）
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.userService.Update>[0]['data']> & {
        password?: string;
      };
    }) => {
      const { password, ...data } = values ?? {};
      return apiClient.userService.Update({
        id,
        data: data,
        password: password ? encryptPassword(String(password)) : undefined,
        updateMask: makeUpdateMask(Object.keys(data)),
      });
    },
    ...options,
  });
}

// ==============================
// 检查用户是否存在
// ==============================
export function useUserExists(
  options?: UseMutationOptions<UserExistsResponse, Error, UserExistsRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.userService.UserExists(data),
    ...options,
  });
}

// ==============================
// 修改用户密码（管理员）
// ==============================
export function useEditUserPassword(
  options?: UseMutationOptions<Record<string, never>, Error, EditUserPasswordRequest>,
) {
  return useMutation({
    // 后端 NeedDecrypt 要求 AES 密文传输（与登录同规），明文会被当密文解密导致校验必败
    mutationFn: (data) =>
      apiClient.userService.EditUserPassword({
        ...data,
        newPassword: data.newPassword ? encryptPassword(data.newPassword) : data.newPassword,
      }),
    ...options,
  });
}
