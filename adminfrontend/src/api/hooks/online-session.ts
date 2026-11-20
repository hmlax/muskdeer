import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationOptions,
  type UseQueryOptions,
} from '@tanstack/react-query';
import {
  type ForceLogoutSessionRequest,
  type ForceLogoutSessionResponse,
  type ListOnlineSessionResponse,
  type RevokeMyOnlineSessionRequest,
  type RevokeMyOnlineSessionResponse,
} from '@/api/generated/admin/v1';
import { queryClient } from '@/core';
import { apiClient } from '@/api/client';

// ==============================
// 在线会话（在线用户 + 强制下线）
// ==============================

const LIST_KEY = 'listOnlineSessions';

export type OnlineSessionListParams = {
  keyword?: string;
  page?: number;
  pageSize?: number;
};

export function useListOnlineSessions(
  params: OnlineSessionListParams,
  options?: Omit<UseQueryOptions<ListOnlineSessionResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: [LIST_KEY, params],
    queryFn: () => apiClient.onlineSessionService.ListOnlineSession(params),
    ...options,
  });
}

export async function fetchListOnlineSessions(params: OnlineSessionListParams) {
  return queryClient.fetchQuery({
    queryKey: [LIST_KEY, params],
    queryFn: () => apiClient.onlineSessionService.ListOnlineSession(params),
    retry: 0,
  });
}

// ==============================
// 个人中心：我的活跃会话（自助视图）
// ==============================

const MY_KEY = 'listMyOnlineSessions';

export function useListMyOnlineSessions(
  options?: Omit<UseQueryOptions<ListOnlineSessionResponse, Error>, 'queryKey' | 'queryFn'>,
) {
  return useQuery({
    queryKey: [MY_KEY],
    queryFn: () => apiClient.onlineSessionService.ListMyOnlineSession({}),
    ...options,
  });
}

export function useRevokeMyOnlineSession(
  options?: UseMutationOptions<RevokeMyOnlineSessionResponse, Error, RevokeMyOnlineSessionRequest>,
) {
  const queryClientRef = useQueryClient();
  return useMutation({
    mutationFn: (req: RevokeMyOnlineSessionRequest) =>
      apiClient.onlineSessionService.RevokeMyOnlineSession(req),
    onSuccess: () => {
      queryClientRef.invalidateQueries({ queryKey: [MY_KEY] });
    },
    ...options,
  });
}

export function useForceLogoutSession(
  options?: UseMutationOptions<ForceLogoutSessionResponse, Error, ForceLogoutSessionRequest>,
) {
  const queryClientRef = useQueryClient();
  return useMutation({
    mutationFn: (req: ForceLogoutSessionRequest) =>
      apiClient.onlineSessionService.ForceLogoutSession(req),
    onSuccess: () => {
      // 被踢会话已从 Redis 删除，刷新在线列表
      queryClientRef.invalidateQueries({ queryKey: [LIST_KEY] });
    },
    ...options,
  });
}
