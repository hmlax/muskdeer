import { AxiosError, AxiosHeaders, type AxiosAdapter } from 'axios';
import { describe, expect, it } from 'vitest';
import { RequestClient } from '@/core/transport/rest/request-client';
import { isTradingAccountDisabled, instanceActionMessageKey, makerCheckMessageKey, accountPolicyMessageKey } from './muskdeer-errors';

describe('trading account start precondition', () => {
  it('recognizes the business reason after the application transport unwraps HTTP 409', async () => {
    const adapter: AxiosAdapter = async (config) => {
      throw new AxiosError('conflict', 'ERR_BAD_REQUEST', config, undefined, {
        config,
        data: { code: 409, reason: 'TRADING_ACCOUNT_DISABLED' },
        status: 409,
        statusText: 'Conflict',
        headers: new AxiosHeaders(),
      });
    };
    const client = new RequestClient({ adapter });
    const failure: unknown = await client.request('/admin/v1/muskdeer/instances/1/start', {
      method: 'POST',
      data: { expectedRevision: 1, expectedControlVersion: 1 },
    }).catch((error: unknown) => error);
    expect(isTradingAccountDisabled(failure)).toBe(true);
  });

  it.each([null, undefined, 'offline', new Error('offline'), { reason: 'MUSKDEER_MANAGEMENT_ERROR' }])('keeps unrelated failures separate: %s', (failure) => {
    expect(isTradingAccountDisabled(failure)).toBe(false);
  });
});

describe('maker check reasons', () => {
  it.each([
    ['MAKER_BUDGET_INSUFFICIENT', 'makerBudgetInsufficient'],
    ['MAKER_SOURCE_UNAVAILABLE', 'makerSourceUnavailable'],
    ['MAKER_SNAPSHOT_UNAVAILABLE', 'makerSnapshotUnavailable'],
    ['MAKER_CHECK_EXPIRED', 'makerCheckExpired'],
    ['TRADING_ACCOUNT_DISABLED', 'enableAccountBeforeStart'],
  ])('keeps actionable start failure %s', (reason, key) => {
    expect(instanceActionMessageKey({ code: 409, reason })).toBe(key);
    expect(makerCheckMessageKey(reason)).toBe(key);
  });
  it('does not report unknown or transport failures as passed checks', () => {
    expect(instanceActionMessageKey(new Error('offline'))).toBe('failed');
    expect(makerCheckMessageKey(undefined)).toBe('failed');
  });
});

describe('account policy validation', () => {
  it.each([
    ['ACCOUNT_POLICY_CONFLICT', 'accountPolicyConflict'],
    ['ACCOUNT_POLICY_EXPOSURE', 'accountPolicyExposure'],
    ['ACCOUNT_POLICY_ORDER_LEVERAGE', 'accountPolicyOrderLeverage'],
    ['ACCOUNT_POLICY_POSITION_LEVERAGE', 'accountPolicyPositionLeverage'],
    ['ACCOUNT_POLICY_GRID_LIMIT', 'accountPolicyGridLimit'],
    ['ACCOUNT_POLICY_SNAPSHOT_UNAVAILABLE', 'accountPolicySnapshotUnavailable'],
  ])('explains account precondition %s', (reason, key) => {
    expect(accountPolicyMessageKey({ code: 409, reason })).toBe(key);
  });
  it('asks to verify an unconfirmed save without assuming rollback', () => {
    expect(accountPolicyMessageKey(new Error('connection reset'))).toBe('accountPolicyFailed');
  });
});
