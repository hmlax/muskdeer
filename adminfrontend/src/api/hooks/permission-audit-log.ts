import { useQuery, type UseQueryOptions } from '@tanstack/react-query';
import {
  type GetPermissionAuditLogRequest,
  type ListPermissionAuditLogResponse,
  type PermissionAuditLog,
} from '@/api/generated/admin/v1';
import { type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 权限审计日志
// ==============================

export function useListPermissionAuditLogs(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListPermissionAuditLogResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listPermissionAuditLogs', query],
    queryFn: () => apiClient.permissionAuditLogService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListPermissionAuditLogs(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listPermissionAuditLogs', params],
    queryFn: () => apiClient.permissionAuditLogService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetPermissionAuditLog(
  req: GetPermissionAuditLogRequest,
  options?: Omit<UseQueryOptions<PermissionAuditLog, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getPermissionAuditLog', req],
    queryFn: () => apiClient.permissionAuditLogService.Get(req),
    ...options,
  });
}
