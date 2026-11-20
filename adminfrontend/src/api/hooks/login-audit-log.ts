import { useQuery, type UseQueryOptions } from '@tanstack/react-query';
import {
  type GetLoginAuditLogRequest,
  type ListLoginAuditLogResponse,
  type LoginAuditLog,
} from '@/api/generated/admin/v1';
import { type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 登录审计日志
// ==============================

export function useListLoginAuditLogs(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListLoginAuditLogResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listLoginAuditLogs', query],
    queryFn: () => apiClient.loginAuditLogService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListLoginAuditLogs(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listLoginAuditLogs', params],
    queryFn: () => apiClient.loginAuditLogService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetLoginAuditLog(
  req: GetLoginAuditLogRequest,
  options?: Omit<UseQueryOptions<LoginAuditLog, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getLoginAuditLog', req],
    queryFn: () => apiClient.loginAuditLogService.Get(req),
    ...options,
  });
}
