import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '@/api/client';
import type { MuskdeerAccountDraft, MuskdeerInstanceDraft, MuskdeerInstance, MuskdeerRunningRequest, MuskdeerCancelRequest, MuskdeerBindingDraft, MuskdeerSetBindingEnabledRequest } from '@/api/generated/admin/v1';
import { useUserStore } from '@/stores/user';

export const muskdeerReaders = {
 gateways: () => apiClient.muskdeerService.ListGateways({}),
 templates: apiClient.muskdeerService.ListTemplates,
 instances: apiClient.muskdeerService.ListInstances,
 instruments: apiClient.muskdeerService.ListInstruments,
 accounts: apiClient.muskdeerService.ListAccounts,
 orders: apiClient.muskdeerService.ListOrderRecords,
 cancellations: apiClient.muskdeerService.ListCancellations,
 bindings: apiClient.muskdeerService.ListAccountBindings,
};
export function useMuskdeerScope() {
 const id = useUserStore((s) => s.userInfo?.id);
 const tenant = useUserStore((s) => s.userInfo?.tenantId);
 return `${tenant ?? 0}:${id ?? 0}`;
}
export function useMuskdeerOverview() {
 const scope = useMuskdeerScope();
 return useQuery({ queryKey: ['muskdeer', scope, 'overview'], queryFn: () => apiClient.muskdeerService.GetOverview({}), retry: 0, refetchInterval: 10000 });
}
export function useMuskdeerSnapshot(accountId: string) {
 const scope = useMuskdeerScope();
 return useQuery({ queryKey: ['muskdeer', scope, 'snapshot', accountId], queryFn: () => apiClient.muskdeerService.GetAccountSnapshot({ accountId }), enabled: !!accountId, retry: 0, refetchInterval: 10000 });
}
export function useMuskdeerWrite<T, R>(write: (input: T) => Promise<R>) {
 const queryClient = useQueryClient();
 return useMutation({ mutationFn: write, retry: 0,
  onSuccess: () => { void queryClient.invalidateQueries({ queryKey: ['muskdeer'] }); },
  onError: (error) => { console.error('Muskdeer operation failed', error); },
 });
}
export const saveMuskdeerInstance = (input: { draft: MuskdeerInstanceDraft; current?: MuskdeerInstance }) => input.current
 ? apiClient.muskdeerService.UpdateInstance({ id: input.current.id, expectedRevision: input.current.revision, expectedControlVersion: input.current.controlVersion, data: { parameters: input.draft.parameters } })
 : apiClient.muskdeerService.CreateInstance({ data: input.draft });
export const startMuskdeerInstance = (input: MuskdeerRunningRequest) => apiClient.muskdeerService.StartInstance(input);
export const stopMuskdeerInstance = (input: MuskdeerRunningRequest) => apiClient.muskdeerService.StopInstance(input);
export const cancelMuskdeerOrders = (input: MuskdeerCancelRequest) => apiClient.muskdeerService.CancelInstanceOrders(input);
export const createMuskdeerBinding = (data: MuskdeerBindingDraft) => apiClient.muskdeerService.CreateAccountBinding({ data });
export const setMuskdeerBindingEnabled = (input: MuskdeerSetBindingEnabledRequest) => apiClient.muskdeerService.SetAccountBindingEnabled(input);
export const getMuskdeerInstance = (id: number) => apiClient.muskdeerService.GetInstance({ id });
export const createMuskdeerAccount = (data: MuskdeerAccountDraft) => apiClient.muskdeerService.CreateAccount({ data });
