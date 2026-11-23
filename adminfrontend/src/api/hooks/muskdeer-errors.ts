export function isTradingAccountDisabled(error: unknown): boolean {
  return error !== null && typeof error === 'object'
    && 'reason' in error && error.reason === 'TRADING_ACCOUNT_DISABLED';
}

export function makerCheckMessageKey(reason: unknown): string {
  switch (reason) {
    case 'MAKER_CHECK_PASSED': return 'makerCheckPassed';
    case 'TRADING_ACCOUNT_DISABLED': return 'enableAccountBeforeStart';
    case 'MAKER_BUDGET_INSUFFICIENT': return 'makerBudgetInsufficient';
    case 'MAKER_SNAPSHOT_UNAVAILABLE': return 'makerSnapshotUnavailable';
    case 'MAKER_SOURCE_UNAVAILABLE': return 'makerSourceUnavailable';
    case 'MAKER_TARGETS_INVALID': return 'makerTargetsInvalid';
    case 'MAKER_REVISION_NOT_LOADED': return 'restartRequired';
    case 'MAKER_CHECK_EXPIRED': return 'makerCheckExpired';
    default: return 'failed';
  }
}

export function instanceActionMessageKey(error: unknown): string {
  return makerCheckMessageKey(error !== null && typeof error === 'object' && 'reason' in error ? error.reason : undefined);
}
