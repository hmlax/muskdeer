import {
  useMutation,
  type UseMutationOptions,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type User,
  type ChangePasswordRequest,
  type UploadAvatarRequest,
  type UploadAvatarResponse,
  type BindContactRequest,
  type VerifyContactRequest,
} from '@/api/generated/admin/v1';
import { apiClient } from '@/api/client';
import { makeUpdateMask, queryClient } from '@/core';
import { encryptPassword } from '@/utils';

/**
 * 获取当前用户
 */
async function getMe(): Promise<User | null> {
  return apiClient.userProfileService.GetUser({});
}

export function useGetUserProfile(
  options?: Omit<UseQueryOptions<User | null, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: ['getMe'],
    queryFn: () => getMe(),
    ...options,
  });
}

// ==============================================
// 获取用户资料 【给 Store / 外部调用】不带 Hook 的方法
// ==============================================
export async function fetchUserProfile() {
  return queryClient.fetchQuery({
    queryKey: ['userProfile'],
    queryFn: () => getMe(),
    retry: 0,
  });
}

export function useUpdateUserProfile(
  options?: UseMutationOptions<
    Record<string, never>,
    Error,
    {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.userProfileService.UpdateUser>[0]['data']>;
    }
  >,
) {
  return useMutation({
    mutationFn: ({
      id,
      values,
    }: {
      id: number;
      values: NonNullable<Parameters<typeof apiClient.userProfileService.UpdateUser>[0]['data']>;
    }) =>
      apiClient.userProfileService.UpdateUser({
        id,
        data: { ...values },
        updateMask: makeUpdateMask(Object.keys(values ?? {})),
      }),
    ...options,
  });
}

export function useChangePassword(
  options?: UseMutationOptions<Record<string, never>, Error, ChangePasswordRequest>,
) {
  return useMutation({
    // 后端 NeedDecrypt 要求 AES 密文传输（与登录同规），明文会被当密文解密导致校验必败
    mutationFn: (data) =>
      apiClient.userProfileService.ChangePassword({
        oldPassword: encryptPassword(data.oldPassword ?? ''),
        newPassword: encryptPassword(data.newPassword ?? ''),
      }),
    ...options,
  });
}

export function useUploadAvatar(
  options?: UseMutationOptions<UploadAvatarResponse, Error, UploadAvatarRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.userProfileService.UploadAvatar(data),
    ...options,
  });
}

export function useDeleteAvatar(options?: UseMutationOptions<Record<string, never>, Error, void>) {
  return useMutation({
    mutationFn: () => apiClient.userProfileService.DeleteAvatar({}),
    ...options,
  });
}

export function useBindContact(
  options?: UseMutationOptions<Record<string, never>, Error, BindContactRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.userProfileService.BindContact(data),
    ...options,
  });
}

export function useVerifyContact(
  options?: UseMutationOptions<Record<string, never>, Error, VerifyContactRequest>,
) {
  return useMutation({
    mutationFn: (data) => apiClient.userProfileService.VerifyContact(data),
    ...options,
  });
}
