import { useMutation, useQuery, type UseMutationOptions } from '@tanstack/react-query';
import {
  type DisableMFARequest,
  type GetMFAStatusResponse,
  type StartEnrollMethodRequest,
  type StartEnrollMethodResponse,
  type ConfirmEnrollMethodRequest,
  type ConfirmEnrollMethodResponse,
} from '@/api/generated/admin/v1';
import { apiClient } from '@/api/client';

/**
 * 获取当前用户 MFA 状态（是否已绑定、已注册因子列表，不含 secret）。
 */
async function getMfaStatus(): Promise<GetMFAStatusResponse | null> {
  return apiClient.mfaService.GetMFAStatus({});
}

export function useGetMfaStatus(enabled = true) {
  return useQuery({
    queryKey: ['mfa-status'],
    queryFn: () => getMfaStatus(),
    enabled,
  });
}

/**
 * 开始注册 TOTP（后端返回 secret + otpauth URI + QR dataURI + operation_id）。
 */
export function useStartEnrollMfa(
  options?: UseMutationOptions<StartEnrollMethodResponse, Error, StartEnrollMethodRequest>,
) {
  return useMutation({
    mutationFn: (req: StartEnrollMethodRequest) => apiClient.mfaService.StartEnrollMethod(req),
    ...options,
  });
}

/**
 * 确认注册 TOTP：提交首码完成绑定。
 */
export function useConfirmEnrollMfa(
  options?: UseMutationOptions<ConfirmEnrollMethodResponse, Error, ConfirmEnrollMethodRequest>,
) {
  return useMutation({
    mutationFn: (req: ConfirmEnrollMethodRequest) => apiClient.mfaService.ConfirmEnrollMethod(req),
    ...options,
  });
}

/**
 * 禁用/移除当前用户的 MFA 凭证。
 */
export function useDisableMfa(
  options?: UseMutationOptions<Record<string, never>, Error, DisableMFARequest>,
) {
  return useMutation({
    mutationFn: (req: DisableMFARequest) => apiClient.mfaService.DisableMFA(req),
    ...options,
  });
}

/**
 * 管理端救援重置：清空目标用户指定方法（TOTP）的全部 MFA 因子。
 * 仅平台管理员可调用（后端强制校验）。
 */
export function useAdminResetMfa(
  options?: UseMutationOptions<Record<string, never>, Error, DisableMFARequest>,
) {
  return useMutation({
    mutationFn: (req: DisableMFARequest) => apiClient.mfaService.DisableMFA(req),
    ...options,
  });
}
