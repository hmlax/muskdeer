import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type Api,
  type CreateApiRequest,
  type DeleteApiRequest,
  type GetApiRequest,
  type ListApiResponse,
} from '@/api/generated/admin/v1';
import { makeUpdateMask, type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// API 管理
// ==============================

export function useListApis(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListApiResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listApis', query],
    queryFn: () => apiClient.apiService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListApis(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listApis', params],
    queryFn: () => apiClient.apiService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetApi(
  req: GetApiRequest,
  options?: Omit<UseQueryOptions<Api, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getApi', req],
    queryFn: () => apiClient.apiService.Get(req),
    ...options,
  });
}

export function useCreateApi(
  options?: UseMutationOptions<Record<string, never>, Error, CreateApiRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.apiService.Create(data),
    ...options,
  });
}

export function useUpdateApi(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    { id: number; values: NonNullable<Parameters<typeof apiClient.apiService.Update>[0]['data']> }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.apiService.Update>[0]['data']>;
    }) =>
      apiClient.apiService.Update({
        id,
        data: {
          ...values,
        },
        updateMask: makeUpdateMask(Object.keys(values ?? [])),
      }),
    ...options,
  });
}

export function useDeleteApi(
  options?: UseMutationOptions<Record<string, never>, Error, DeleteApiRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.apiService.Delete(data),
    ...options,
  });
}

export function useSyncApisApi(options?: UseMutationOptions<Record<string, never>, Error>) {
  return useMutation({
    mutationFn: () => apiClient.apiService.SyncApis({}),
    ...options,
  });
}
