import { validateMerchantCode } from './merchant-code';
export { validateMerchantCode } from './merchant-code';

export type AdminApplication = 'platform' | 'merchant';
export const TENANT_CODE_HEADER = 'X-Tenant-Code';
export const adminApplication: AdminApplication =
  import.meta.env.VITE_ADMIN_APP === 'merchant' ? 'merchant' : 'platform';

let merchantCode = '';

export async function initializeApplication(): Promise<void> {
  if (adminApplication === 'platform') return;
  const response = await fetch('/app-config.json', { cache: 'no-store' });
  if (!response.ok)
    throw new Error(`Failed to load merchant deployment configuration: ${response.status}`);
  const config: { merchantCode?: string } = await response.json();
  merchantCode = validateMerchantCode(config.merchantCode);
}

export function getMerchantCode(): string {
  if (adminApplication === 'platform') return '';
  return validateMerchantCode(merchantCode);
}

export function applicationHeaders(): Record<string, string> {
  const code = getMerchantCode();
  return code ? { [TENANT_CODE_HEADER]: code } : {};
}

export function applicationStorageKey(name: string): string {
  const code = getMerchantCode();
  return `${adminApplication}-admin${code ? `:${code}` : ''}:${name}-storage`;
}

export function refreshExpiryCookieName(): string {
  const code = getMerchantCode();
  return code ? `refresh_exp_${code}` : 'refresh_exp';
}
