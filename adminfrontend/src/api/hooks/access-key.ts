import { useMutation, type UseMutationOptions } from '@tanstack/react-query';
import { apiClient } from '@/api/client';
import { type PaginationQuery, queryClient } from '@/core';
import { makeUpdateMask } from '@/core/transport/rest/utils';
import type {
  AccessKey as AccessKey,
  CreateAccessKeyRequest,
  CreateAccessKeyResponse,
  DeleteAccessKeyRequest,
  ResetAccessKeySecretRequest,
} from '@/api/generated/admin/v1';

/** 非组件上下文取列表（ProTable request / 菜单同步构建器） */
export async function fetchListAccessKeys(params: PaginationQuery) {
  return queryClient.fetchQuery({
    queryKey: ['listAccessKeys', params],
    queryFn: () => apiClient.accessKeyService.List(params.toRawParams()),
    retry: 0,
  });
}

/** 创建凭证：响应含一次性明文 secret */
export function useCreateAccessKey(
  options?: UseMutationOptions<CreateAccessKeyResponse, Error, CreateAccessKeyRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.accessKeyService.Create(data),
    ...options,
  });
}

/** 更新凭证（名称/状态/过期时间）；服务端黑名单保护 access_key/secret_hash/tenant_id */
export function useUpdateAccessKey(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.accessKeyService.Update>[0]['data']>;
    }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.accessKeyService.Update>[0]['data']>;
    }) =>
      apiClient.accessKeyService.Update({
        id,
        data: { ...values },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useDeleteAccessKey(
  options?: UseMutationOptions<Record<string, never>, Error, DeleteAccessKeyRequest>,
) {
  return useMutation({
    mutationFn: (req) => apiClient.accessKeyService.Delete(req),
    ...options,
  });
}

/** 重置密钥：生成新 SK 明文返回一次（旧 SK 立即失效于交换） */
export function useResetAccessKeySecret(
  options?: UseMutationOptions<CreateAccessKeyResponse, Error, ResetAccessKeySecretRequest>,
) {
  return useMutation({
    mutationFn: (req) => apiClient.accessKeyService.ResetSecret(req),
    ...options,
  });
}

export type { AccessKey };
