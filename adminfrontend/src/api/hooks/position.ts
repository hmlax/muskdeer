import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type CreatePositionRequest,
  type DeletePositionRequest,
  type GetPositionRequest,
  type ListPositionResponse,
  type Position,
} from '@/api/generated/admin/v1';
import { makeUpdateMask, type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 职位管理
// ==============================

export function useListPositions(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListPositionResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listPositions', query],
    queryFn: () => apiClient.positionService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListPositions(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listPositions', params],
    queryFn: () => apiClient.positionService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetPosition(
  req: GetPositionRequest,
  options?: Omit<UseQueryOptions<Position, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getPosition', req],
    queryFn: () => apiClient.positionService.Get(req),
    ...options,
  });
}

export function useCreatePosition(
  options?: UseMutationOptions<Record<string, never>, Error, CreatePositionRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.positionService.Create(data),
    ...options,
  });
}

export function useUpdatePosition(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.positionService.Update>[0]['data']>;
    }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.positionService.Update>[0]['data']>;
    }) =>
      apiClient.positionService.Update({
        id,
        data: { ...values },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useDeletePosition(
  options?: UseMutationOptions<Record<string, never>, Error, DeletePositionRequest>,
) {
  return useMutation({
    mutationFn: (req) => apiClient.positionService.Delete(req),
    ...options,
  });
}
