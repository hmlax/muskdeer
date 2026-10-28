import { ApiError, managementJSON, managementWriteJSON } from './api';
import { sources, states } from './strategy-api';
import type { Source, State } from './strategy-api';

export const actions = {
  'strategy.start': { title: '启动', target: 'RUNNING', description: '使用当前请求配置和所选行情源启动；通过风险校验后开始报价。' },
  'strategy.pause': { title: '暂停', target: 'PAUSED', description: '暂停报价并撤回挂单，等待订单确认终态后完成。' },
  'strategy.resume': { title: '恢复', target: 'RUNNING', description: '按当前请求配置和所选行情源恢复报价。' },
  'strategy.stop': { title: '停止', target: 'STOPPED', description: '停止报价并等待订单确认终态。此操作不会发起平仓。' },
} as const;
export const operationStates = { ACCEPTED: '请求已受理', SUCCEEDED: '已完成', SUPERSEDED: '已被新目标替代' } as const;
export type Action = keyof typeof actions;
export type OperationState = keyof typeof operationStates;
export interface Observation { strategy_revision: string; state: State; runtime_owner_id: string; runtime_owner_epoch: string }
export interface Operation {
  id: string; actor_user_id: string; strategy_id: string; action: Action; target_state: State;
  expected_revision: string; effect_revision: string; config_version: string; market_data_source: Source;
  state: OperationState; outcome_code: string; reason: string; revision: string; created_at_ms: string;
  completed_at_ms: string | null; observation: Observation | null;
}
export interface ControlRequest { strategy_id: string; action: Action; expected_revision: string; reason: string; key: string }

function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new ApiError('INVALID_RESPONSE');
  return value as Record<string, unknown>;
}
function text(value: unknown, maximum = 256): string {
  if (typeof value !== 'string' || value.length > maximum) throw new ApiError('INVALID_RESPONSE');
  return value;
}
function integer(value: unknown, zero = false): string {
  const input = text(value);
  if (!/^(0|[1-9][0-9]{0,18})$/.test(input) || BigInt(input) > 9223372036854775807n || (!zero && input === '0')) throw new ApiError('INVALID_RESPONSE');
  return input;
}
function state(value: unknown): State {
  if (typeof value !== 'string' || !Object.hasOwn(states, value)) throw new ApiError('INVALID_RESPONSE');
  return value as State;
}
function operation(value: unknown): Operation {
  const p = object(value);
  if (typeof p.action !== 'string' || !Object.hasOwn(actions, p.action) || typeof p.state !== 'string' || !Object.hasOwn(operationStates, p.state) || typeof p.market_data_source !== 'string' || !Object.hasOwn(sources, p.market_data_source)) throw new ApiError('INVALID_RESPONSE');
  const result: Operation = {
    id: integer(p.id), actor_user_id: integer(p.actor_user_id), strategy_id: integer(p.strategy_id), action: p.action as Action,
    target_state: state(p.target_state), expected_revision: integer(p.expected_revision), effect_revision: integer(p.effect_revision),
    config_version: integer(p.config_version), market_data_source: p.market_data_source as Source,
    state: p.state as OperationState, outcome_code: text(p.outcome_code), reason: text(p.reason), revision: integer(p.revision),
    created_at_ms: integer(p.created_at_ms), completed_at_ms: p.completed_at_ms == null ? null : integer(p.completed_at_ms), observation: null,
  };
  if (BigInt(result.effect_revision) !== BigInt(result.expected_revision) + 1n || result.target_state !== actions[result.action].target) throw new ApiError('INVALID_RESPONSE');
  if (result.state === 'ACCEPTED') {
    if (result.revision !== '1' || result.outcome_code !== 'pending' || result.completed_at_ms !== null || p.observation != null) throw new ApiError('INVALID_RESPONSE');
  } else {
    const proof = object(p.observation);
    result.observation = { strategy_revision: integer(proof.strategy_revision), state: state(proof.state), runtime_owner_id: text(proof.runtime_owner_id, 128), runtime_owner_epoch: integer(proof.runtime_owner_epoch, true) };
    if (result.revision !== '2' || result.completed_at_ms === null || BigInt(result.completed_at_ms) < BigInt(result.created_at_ms) || BigInt(result.observation.strategy_revision) < BigInt(result.effect_revision) ||
        result.state === 'SUCCEEDED' && (result.outcome_code !== 'target_reached' || result.observation.state !== result.target_state || result.target_state === 'RUNNING' && (result.observation.runtime_owner_epoch === '0' || result.observation.runtime_owner_id === '')) || result.state === 'SUPERSEDED' && result.outcome_code !== 'target_replaced') throw new ApiError('INVALID_RESPONSE');
  }
  return result;
}

export async function getOperation(id: string, signal: AbortSignal): Promise<Operation> {
  integer(id);
  const result = operation(await managementJSON(`operations/${id}`, signal));
  if (result.id !== id) throw new ApiError('INVALID_RESPONSE');
  return result;
}
export async function controlStrategy(request: ControlRequest, signal: AbortSignal): Promise<Operation> {
  integer(request.strategy_id); integer(request.expected_revision);
  const result = operation(await managementWriteJSON(`strategies/${request.strategy_id}/control`, request.key, {
    action: request.action, expected_revision: request.expected_revision, reason: request.reason,
  }, signal));
  if (result.strategy_id !== request.strategy_id || result.action !== request.action || result.expected_revision !== request.expected_revision || result.reason !== request.reason) throw new ApiError('INVALID_RESPONSE');
  return result;
}
