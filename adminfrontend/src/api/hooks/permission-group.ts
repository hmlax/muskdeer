import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type CreatePermissionGroupRequest,
  type DeletePermissionGroupRequest,
  type GetPermissionGroupRequest,
  type ListPermissionGroupResponse,
  type PermissionGroup,
} from '@/api/generated/admin/v1';
import { makeUpdateMask, type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 权限组管理
// ==============================

export function useListPermissionGroups(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListPermissionGroupResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listPermissionGroups', query],
    queryFn: () => apiClient.permissionGroupService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListPermissionGroups(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listPermissionGroups', params],
    queryFn: () => apiClient.permissionGroupService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetPermissionGroup(
  req: GetPermissionGroupRequest,
  options?: Omit<UseQueryOptions<PermissionGroup, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getPermissionGroup', req],
    queryFn: () => apiClient.permissionGroupService.Get(req),
    ...options,
  });
}

export function useCreatePermissionGroup(
  options?: UseMutationOptions<Record<string, never>, Error, CreatePermissionGroupRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.permissionGroupService.Create(data),
    ...options,
  });
}

export function useUpdatePermissionGroup(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.permissionGroupService.Update>[0]['data']>;
    }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.permissionGroupService.Update>[0]['data']>;
    }) =>
      apiClient.permissionGroupService.Update({
        id,
        data: { ...values, children: [] },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useDeletePermissionGroup(
  options?: UseMutationOptions<Record<string, never>, Error, DeletePermissionGroupRequest>,
) {
  return useMutation({
    mutationFn: (req) => apiClient.permissionGroupService.Delete(req),
    ...options,
  });
}
