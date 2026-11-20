import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type CreateOrgUnitRequest,
  type DeleteOrgUnitRequest,
  type GetOrgUnitRequest,
  type ListOrgUnitResponse,
  type OrgUnit,
} from '@/api/generated/admin/v1';
import { makeUpdateMask, type PaginationQuery, queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 组织架构管理
// ==============================

export function useListOrgUnits(
  query: PaginationQuery,
  options?: Omit<UseQueryOptions<ListOrgUnitResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['listOrgUnits', query],
    queryFn: () => apiClient.orgUnitService.List(query.toRawParams()),
    ...options,
  });
}

export async function fetchListOrgUnits(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listOrgUnits', params],
    queryFn: () => apiClient.orgUnitService.List(params.toRawParams()),
    retry: 0,
  });
}

export function useGetOrgUnit(
  req: GetOrgUnitRequest,
  options?: Omit<UseQueryOptions<OrgUnit, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getOrgUnit', req],
    queryFn: () => apiClient.orgUnitService.Get(req),
    ...options,
  });
}

export function useCreateOrgUnit(
  options?: UseMutationOptions<Record<string, never>, Error, CreateOrgUnitRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.orgUnitService.Create(data),
    ...options,
  });
}

export function useUpdateOrgUnit(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.orgUnitService.Update>[0]['data']>;
    }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.orgUnitService.Update>[0]['data']>;
    }) =>
      apiClient.orgUnitService.Update({
        id,
        data: { ...values },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useDeleteOrgUnit(
  options?: UseMutationOptions<Record<string, never>, Error, DeleteOrgUnitRequest>,
) {
  return useMutation({
    mutationFn: (req) => apiClient.orgUnitService.Delete(req),
    ...options,
  });
}
