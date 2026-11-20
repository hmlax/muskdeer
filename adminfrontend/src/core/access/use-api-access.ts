import { useCallback } from 'react';
import { useUserStore } from '@/stores/user';
import { canAccessApi } from './api-permission';

export function useApiAccess() {
  const grants = useUserStore((state) => state.apiPermissions);
  return useCallback((method: string, path: string) => canAccessApi(grants, method, path), [grants]);
}
