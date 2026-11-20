import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type CreateLanguageRequest,
  type DeleteLanguageRequest,
  type GetLanguageRequest,
  type Language,
  type ListLanguageResponse,
  type BatchCreateLanguagesRequest,
} from '@/api/generated/admin/v1';
import { makeUpdateMask, type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 语言管理
// ==============================

export function useListLanguages(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListLanguageResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listLanguages', query],
    queryFn: () => apiClient.languageService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListLanguages(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listLanguages', params],
    queryFn: () => apiClient.languageService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetLanguage(
  req: GetLanguageRequest,
  options?: Omit<UseQueryOptions<Language, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getLanguage', req],
    queryFn: () => apiClient.languageService.Get(req),
    ...options,
  });
}

export function useCreateLanguage(
  options?: UseMutationOptions<Record<string, never>, Error, CreateLanguageRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.languageService.Create(data),
    ...options,
  });
}

export function useUpdateLanguage(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.languageService.Update>[0]['data']>;
    }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.languageService.Update>[0]['data']>;
    }) =>
      apiClient.languageService.Update({
        id,
        data: { ...values },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useDeleteLanguage(
  options?: UseMutationOptions<Record<string, never>, Error, DeleteLanguageRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.languageService.Delete(data),
    ...options,
  });
}

export function useBatchCreateLanguages(
  options?: UseMutationOptions<Record<string, never>, Error, BatchCreateLanguagesRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.languageService.BatchCreate(data),
    ...options,
  });
}
