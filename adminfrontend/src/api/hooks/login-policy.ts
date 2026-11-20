import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type DeleteLoginPolicyRequest,
  type GetLoginPolicyRequest,
  type ListLoginPolicyResponse,
  type LoginPolicy,
} from '@/api/generated/admin/v1';
import { makeUpdateMask, type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 登录策略管理
// ==============================

export function useListLoginPolicies(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListLoginPolicyResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listLoginPolicies', query],
    queryFn: () => apiClient.loginPolicyService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListLoginPolicies(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listLoginPolicies', params],
    queryFn: () => apiClient.loginPolicyService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetLoginPolicy(
  req: GetLoginPolicyRequest,
  options?: Omit<UseQueryOptions<LoginPolicy, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getLoginPolicy', req],
    queryFn: () => apiClient.loginPolicyService.Get(req),
    ...options,
  });
}

export function useCreateLoginPolicy(
  options?: UseMutationOptions<LoginPolicy, Error, LoginPolicy>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.loginPolicyService.Create({ data }),
    ...options,
  });
}

export function useUpdateLoginPolicy(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.loginPolicyService.Update>[0]['data']>;
    }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.loginPolicyService.Update>[0]['data']>;
    }) =>
      apiClient.loginPolicyService.Update({
        id,
        data: { ...values },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useDeleteLoginPolicy(
  options?: UseMutationOptions<Record<string, never>, Error, DeleteLoginPolicyRequest>,
) {
  return useMutation({
    mutationFn: (req) => apiClient.loginPolicyService.Delete(req),
    ...options,
  });
}
