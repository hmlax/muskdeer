import { useQuery, type UseQueryOptions } from '@tanstack/react-query';
import {
  type DataAccessAuditLog,
  type GetDataAccessAuditLogRequest,
  type ListDataAccessAuditLogResponse,
} from '@/api/generated/admin/v1';
import { type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 数据访问审计日志
// ==============================

export function useListDataAccessAuditLogs(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListDataAccessAuditLogResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listDataAccessAuditLogs', query],
    queryFn: () => apiClient.dataAccessAuditLogService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListDataAccessAuditLogs(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listDataAccessAuditLogs', params],
    queryFn: () => apiClient.dataAccessAuditLogService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetDataAccessAuditLog(
  req: GetDataAccessAuditLogRequest,
  options?: Omit<UseQueryOptions<DataAccessAuditLog, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getDataAccessAuditLog', req],
    queryFn: () => apiClient.dataAccessAuditLogService.Get(req),
    ...options,
  });
}
