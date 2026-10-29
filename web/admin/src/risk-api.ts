import { ApiError, managementJSON, managementWriteJSON } from './api';
import { integer, object, sources, states, text } from './strategy-api';
import type { Source, State, Strategy } from './strategy-api';

export const riskActions = {
  'risk.strategy.block': '关闭新风险',
  'risk.strategy.unblock': '恢复新风险',
} as const;
export type RiskAction = keyof typeof riskActions;
export interface RiskState {
  strategy_id: string; enabled: boolean; revision: string; quote_asset: string;
  limit_notional: string; reserved_notional: string; state: State; desired_state: Strategy['desired_state'];
  requested_config_version: string; market_data_source: Source; updated_at_ms: string;
}
export interface RiskInput {
  action: RiskAction; expected_revision: string; reason: string;
  expected_config_version?: string; expected_desired_state?: Strategy['desired_state'];
}
export interface RiskReceipt {
  id: string; actor_user_id: string; strategy_id: string; action: RiskAction;
  previous_enabled: boolean; enabled: boolean; previous_revision: string; revision: string;
  expected_config_version: string; expected_desired_state: string; reason: string; created_at_ms: string;
}
export interface RiskRequest { strategy_id: string; body: RiskInput; key: string }

function ledger(value: unknown, zero = true): string {
  const result = text(value);
  if (!/^(0|[1-9][0-9]{0,39})(\.[0-9]{1,36})?$/.test(result) || !zero && /^0(?:\.0+)?$/.test(result)) throw new ApiError('INVALID_RESPONSE');
  return result;
}

// Validation compares bounded decimal text; financial values never become Number.
function atMost(left: string, right: string): boolean {
  const [a, af = ''] = left.split('.'); const [b, bf = ''] = right.split('.');
  if (a.length !== b.length) return a.length < b.length;
  if (a !== b) return a < b;
  return af.padEnd(36, '0') <= bf.padEnd(36, '0');
}

function boolean(value: unknown): boolean {
  if (typeof value !== 'boolean') throw new ApiError('INVALID_RESPONSE');
  return value;
}

export async function getRiskState(id: string, signal: AbortSignal): Promise<RiskState> {
  integer(id);
  const p = object(await managementJSON(`strategies/${id}/risk`, signal));
  if (integer(p.strategy_id) !== id || typeof p.state !== 'string' || !Object.hasOwn(states, p.state) ||
      (p.desired_state !== 'RUNNING' && p.desired_state !== 'PAUSED' && p.desired_state !== 'STOPPED') ||
      typeof p.market_data_source !== 'string' || !Object.hasOwn(sources, p.market_data_source)) throw new ApiError('INVALID_RESPONSE');
  const limit = ledger(p.limit_notional, false); const reserved = ledger(p.reserved_notional); const asset = text(p.quote_asset);
  if (!atMost(reserved, limit) || !asset || new TextEncoder().encode(asset).length > 32 || asset.trim() !== asset || asset.includes('\0')) throw new ApiError('INVALID_RESPONSE');
  return {
    strategy_id: id, enabled: boolean(p.enabled), revision: integer(p.revision), quote_asset: asset,
    limit_notional: limit, reserved_notional: reserved, state: p.state as State, desired_state: p.desired_state,
    requested_config_version: integer(p.requested_config_version), market_data_source: p.market_data_source as Source, updated_at_ms: integer(p.updated_at_ms),
  };
}

export async function changeRisk(request: RiskRequest, signal: AbortSignal): Promise<RiskReceipt> {
  integer(request.strategy_id); integer(request.body.expected_revision);
  const p = object(await managementWriteJSON(`strategies/${request.strategy_id}/risk`, request.key, request.body, signal));
  const before = integer(p.previous_revision); const after = integer(p.revision);
  if (integer(p.strategy_id) !== request.strategy_id || p.action !== request.body.action || before !== request.body.expected_revision ||
      BigInt(after) !== BigInt(before) + 1n || p.enabled !== (request.body.action === 'risk.strategy.unblock') ||
      integer(p.expected_config_version, true) !== (request.body.expected_config_version ?? '0') ||
      p.expected_desired_state !== (request.body.expected_desired_state ?? '') || p.reason !== request.body.reason) throw new ApiError('INVALID_RESPONSE');
  return {
    id: integer(p.id), actor_user_id: integer(p.actor_user_id), strategy_id: request.strategy_id, action: request.body.action,
    previous_enabled: boolean(p.previous_enabled), enabled: boolean(p.enabled), previous_revision: before, revision: after,
    expected_config_version: integer(p.expected_config_version, true), expected_desired_state: text(p.expected_desired_state),
    reason: text(p.reason), created_at_ms: integer(p.created_at_ms),
  };
}
