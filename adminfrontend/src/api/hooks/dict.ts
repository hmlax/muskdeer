import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type DictType,
  type ListDictTypeResponse,
  type ListDictEntryResponse,
  type DeleteDictTypeRequest,
  type CreateDictEntryRequest,
  type DeleteDictEntryRequest,
  type CreateDictTypeRequest,
  type GetDictTypeRequest,
} from '@/api/generated/admin/v1';
import { makeUpdateMask, type PaginationQuery } from '@/core/transport/rest';
import { queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 字典类型管理
// ==============================

export function useListDictTypes(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListDictTypeResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listDictTypes', query],
    queryFn: () => apiClient.dictTypeService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListDictTypes(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listDictTypes', params],
    queryFn: () => apiClient.dictTypeService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetDictType(
  req: GetDictTypeRequest,
  options?: Omit<UseQueryOptions<DictType, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getDictType', req],
    queryFn: () => apiClient.dictTypeService.Get(req),
    ...options,
  });
}

export function useCreateDictType(
  options?: UseMutationOptions<DictType, Error, CreateDictTypeRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.dictTypeService.Create(data),
    ...options,
  });
}

export function useUpdateDictType(
  options?: UseMutationOptions<
    DictType,
    Error,
    {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.dictTypeService.Update>[0]['data']>;
    }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.dictTypeService.Update>[0]['data']>;
    }) =>
      apiClient.dictTypeService.Update({
        id,
        data: { ...values },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useDeleteDictType(
  options?: UseMutationOptions<Record<string, never>, Error, DeleteDictTypeRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.dictTypeService.Delete(data),
    ...options,
  });
}

// ==============================
// 字典条目管理
// ==============================

export function useListDictEntries(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListDictEntryResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listDictEntries', query],
    queryFn: () => apiClient.dictEntryService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListDictEntries(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listDictEntries', params],
    queryFn: () => apiClient.dictEntryService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useCreateDictEntry(
  options?: UseMutationOptions<Record<string, never>, Error, CreateDictEntryRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.dictEntryService.Create(data),
    ...options,
  });
}

export function useUpdateDictEntry(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.dictEntryService.Update>[0]['data']>;
    }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.dictEntryService.Update>[0]['data']>;
    }) =>
      apiClient.dictEntryService.Update({
        id,
        data: { ...values },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useDeleteDictEntry(
  options?: UseMutationOptions<Record<string, never>, Error, DeleteDictEntryRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.dictEntryService.Delete(data),
    ...options,
  });
}
