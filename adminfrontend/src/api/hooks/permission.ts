import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type Permission,
  type ListPermissionResponse,
  type GetPermissionRequest,
  type CreatePermissionRequest,
  type DeletePermissionRequest,
} from '@/api/generated/admin/v1';
import { makeUpdateMask, type PaginationQuery } from '@/core/transport/rest';
import { queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 权限点管理
// ==============================

export function useListPermissions(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListPermissionResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listPermissions', query],
    queryFn: () => apiClient.permissionService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListPermissions(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listPermissions', params],
    queryFn: () => apiClient.permissionService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetPermission(
  req: GetPermissionRequest,
  options?: Omit<UseQueryOptions<Permission, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getPermission', req],
    queryFn: () => apiClient.permissionService.Get(req),
    ...options,
  });
}

export function useCreatePermission(
  options?: UseMutationOptions<Record<string, never>, Error, CreatePermissionRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.permissionService.Create(data),
    ...options,
  });
}

export function useUpdatePermission(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.permissionService.Update>[0]['data']>;
    }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.permissionService.Update>[0]['data']>;
    }) =>
      apiClient.permissionService.Update({
        id,
        data: { ...values },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useDeletePermission(
  options?: UseMutationOptions<Record<string, never>, Error, DeletePermissionRequest>,
) {
  return useMutation({
    mutationFn: (req) => apiClient.permissionService.Delete(req),
    ...options,
  });
}

// ==============================
// 同步权限点（按菜单树重建业务权限分组与权限点）
// ==============================

export function useSyncPermissions(options?: UseMutationOptions<Record<string, never>, Error>) {
  return useMutation({
    mutationFn: () => apiClient.permissionService.SyncPermissions({}),
    ...options,
  });
}
