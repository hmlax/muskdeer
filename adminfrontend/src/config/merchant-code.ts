export function validateMerchantCode(value: unknown): string {
  if (
    typeof value !== 'string' ||
    value.length === 0 ||
    value.length > 128 ||
    /[^A-Za-z0-9_-]/.test(value)
  ) {
    throw new Error(
      'Merchant deployment requires a valid merchantCode (VITE_MERCHANT_CODE in development).',
    );
  }
  return value;
}
