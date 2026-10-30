import { ApiError, managementJSON, managementWriteJSON } from './api';
import { integer, object, text } from './strategy-api';
import { orderStates } from './order-api';

export interface CancellationInput { expected_order_revision: string; reason: string }
export interface CancellationRequest { strategy_id: string; order_id: string; key: string; body: CancellationInput }
export interface CancellationReceipt {
  id: string; actor_user_id: string; strategy_id: string; order_id: string; expected_order_revision: string;
  reason: string; state: 'ACCEPTED' | 'SUCCEEDED' | 'FAILED'; outcome_code: string; revision: string;
  created_at_ms: string; completed_at_ms: string | null; observation: { order_revision: string; state: string } | null;
}
export const cancellationStates = { ACCEPTED: '已受理，待确认', SUCCEEDED: '订单已终止', FAILED: '撤单请求未完成' } as const;
function invalid(): never { throw new ApiError('INVALID_RESPONSE'); }
function receipt(input: unknown, strategy: string): CancellationReceipt {
  const p = object(input); const reason = text(p.reason); const state = text(p.state);
  if (integer(p.strategy_id) !== strategy || !Object.hasOwn(cancellationStates, state) || !reason || reason.trim() !== reason || reason.includes('\0') || new TextEncoder().encode(reason).length > 256) invalid();
  const result: CancellationReceipt = {
    id: integer(p.id), actor_user_id: integer(p.actor_user_id), strategy_id: strategy, order_id: integer(p.order_id), expected_order_revision: integer(p.expected_order_revision),
    reason, state: state as CancellationReceipt['state'], outcome_code: text(p.outcome_code), revision: integer(p.revision), created_at_ms: integer(p.created_at_ms), completed_at_ms: null, observation: null,
  };
  if (result.expected_order_revision === '9223372036854775807') invalid();
  if (result.state === 'ACCEPTED') {
    if (result.outcome_code !== 'pending' || p.completed_at_ms !== undefined || p.observation !== null || !['1', '2'].includes(result.revision)) invalid();
  } else {
    const proof = object(p.observation); const observed = text(proof.state);
    result.completed_at_ms = integer(p.completed_at_ms);
    result.observation = { order_revision: integer(proof.order_revision), state: observed };
    if (!['2', '3'].includes(result.revision) || BigInt(result.completed_at_ms) < BigInt(result.created_at_ms) || BigInt(result.observation.order_revision) < BigInt(result.expected_order_revision) || !orderStates.includes(observed as typeof orderStates[number])) invalid();
    const terminal = ['FILLED', 'CANCELED', 'REJECTED'].includes(observed);
    if (result.state === 'SUCCEEDED' ? result.outcome_code !== 'order_terminal' || !terminal : !['cancel_rejected', 'cancel_not_accepted'].includes(result.outcome_code) || terminal) invalid();
  }
  return result;
}
export async function requestCancellation(request: CancellationRequest, signal: AbortSignal): Promise<CancellationReceipt> {
  integer(request.strategy_id); integer(request.order_id); integer(request.body.expected_order_revision);
  const result = receipt(await managementWriteJSON(`strategies/${request.strategy_id}/orders/${request.order_id}/cancellations`, request.key, request.body, signal), request.strategy_id);
  if (result.order_id !== request.order_id || result.expected_order_revision !== request.body.expected_order_revision || result.reason !== request.body.reason) invalid();
  return result;
}
export async function getCancellation(strategy: string, id: string, signal: AbortSignal): Promise<CancellationReceipt> {
  integer(strategy); integer(id);
  const result = receipt(await managementJSON(`strategies/${strategy}/order-cancellations/${id}`, signal), strategy);
  return result.id === id ? result : invalid();
}
