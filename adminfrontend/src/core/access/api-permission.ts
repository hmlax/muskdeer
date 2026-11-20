import type { ApiGrant } from '@/api/generated/admin/v1';

export function canAccessApi(grants: readonly ApiGrant[], method: string, path: string): boolean {
  if (!method || !path) return false;
  const verb = method.toUpperCase();
  return grants.some((grant) =>
    (grant.method === '*' || grant.method?.toUpperCase() === verb) &&
    (grant.path === '*' || grant.path === path),
  );
}
