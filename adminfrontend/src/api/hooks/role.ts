import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type CreateRoleRequest,
  type DeleteRoleRequest,
  type GetRoleRequest,
  type ListRoleResponse,
  type Role,
} from '@/api/generated/admin/v1';
import { makeUpdateMask, type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 角色管理
// ==============================

export function useListRoles(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListRoleResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listRoles', query],
    queryFn: () => apiClient.roleService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListRoles(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listRoles', params],
    queryFn: () => apiClient.roleService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetRole(
  req: GetRoleRequest,
  options?: Omit<UseQueryOptions<Role, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getRole', req],
    queryFn: () => apiClient.roleService.Get(req),
    ...options,
  });
}

export function useCreateRole(
  options?: UseMutationOptions<Record<string, never>, Error, CreateRoleRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.roleService.Create(data),
    ...options,
  });
}

export function useUpdateRole(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    { id: number; values: NonNullable<Parameters<typeof apiClient.roleService.Update>[0]['data']> }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.roleService.Update>[0]['data']>;
    }) =>
      apiClient.roleService.Update({
        id,
        data: { ...values },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useDeleteRole(
  options?: UseMutationOptions<Record<string, never>, Error, DeleteRoleRequest>,
) {
  return useMutation({
    mutationFn: (req) => apiClient.roleService.Delete(req),
    ...options,
  });
}
