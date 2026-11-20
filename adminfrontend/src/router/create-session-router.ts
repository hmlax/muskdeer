import { createAccessibleRouter } from '@/core/router/factory';
import type { GenerateMenuAndRoutesOptions } from '@/core/router/types';
import type { AccessModeType } from '@/core/preferences';
import { adminApplication } from '@/config/application';

export function createSessionRouter(
  preferredMode: AccessModeType,
  session: { authenticated: boolean; user: Pick<BasicUserInfo, 'tenantId'> | null },
  options: GenerateMenuAndRoutesOptions,
) {
  // Tenant menus are granted by server-side role permissions.
  // Saved UI preferences cannot replace that server-controlled route tree.
  const mode =
    session.authenticated &&
    (adminApplication === 'merchant' || !session.user || (session.user.tenantId ?? 0) > 0)
      ? 'backend'
      : preferredMode;
  if (mode === 'backend' && session.authenticated && !options.fetchMenuListAsync) {
    throw new Error('Authenticated server routes require a menu loader');
  }
  return createAccessibleRouter(mode, options);
}
