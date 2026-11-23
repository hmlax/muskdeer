export function isTradingAccountDisabled(error: unknown): boolean {
  return error !== null && typeof error === 'object'
    && 'reason' in error && error.reason === 'TRADING_ACCOUNT_DISABLED';
}
