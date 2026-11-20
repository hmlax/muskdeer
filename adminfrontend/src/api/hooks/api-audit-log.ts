import { useQuery, type UseQueryOptions } from '@tanstack/react-query';
import {
  type ApiAuditLog,
  type GetApiAuditLogRequest,
  type ListApiAuditLogResponse,
} from '@/api/generated/admin/v1';
import { type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// API 审计日志
// ==============================

export function useListApiAuditLogs(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListApiAuditLogResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listApiAuditLogs', query],
    queryFn: () => apiClient.apiAuditLogService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListApiAuditLogs(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listApiAuditLogs', params],
    queryFn: () => apiClient.apiAuditLogService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetApiAuditLog(
  req: GetApiAuditLogRequest,
  options?: Omit<UseQueryOptions<ApiAuditLog, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getApiAuditLog', req],
    queryFn: () => apiClient.apiAuditLogService.Get(req),
    ...options,
  });
}
