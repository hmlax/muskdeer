import { ApiError, managementJSON, managementWriteJSON } from './api';
import { integer, object, text } from './strategy-api';

export const emergencyStates = { ACCEPTED: '已受理，待归结', SUCCEEDED: '范围已归结', FAILED: '范围处理有失败' } as const;
export interface EmergencyInput { scope: 'strategy'; expected_risk_revision: string; reason: string }
export interface EmergencyRequest { strategy_id: string; key: string; body: EmergencyInput }
export interface EmergencyAcceptance {
  id: string; actor_user_id: string; strategy_id: string; scope: 'strategy'; expected_risk_revision: string;
  risk_revision: string; reason: string; created_at_ms: string;
}
export interface EmergencyRecord {
  acceptance: EmergencyAcceptance; state: keyof typeof emergencyStates; revision: string;
  enrollment_complete: boolean; completed_at_ms: string | null;
}
export interface EmergencyProgress { targets: string; terminal: string; failed: string; pending_submissions: string; enrollment_complete: boolean }
export interface EmergencyObservation { record: EmergencyRecord; progress: EmergencyProgress; observed_at_ms: string }
export interface EmergencyHistory { items: EmergencyRecord[]; next_after_id: string; observed_at_ms: string }

function invalid(): never { throw new ApiError('INVALID_RESPONSE'); }
function fields(value: unknown, required: string[], optional: string[] = []): Record<string, unknown> {
  const result = object(value);
  if (required.some(key => !Object.hasOwn(result, key)) || Object.keys(result).some(key => !required.includes(key) && !optional.includes(key))) invalid();
  return result;
}
function boolean(value: unknown): boolean { return typeof value === 'boolean' ? value : invalid(); }
function acceptance(value: unknown, strategy: string): EmergencyAcceptance {
  const p = fields(value, ['id', 'actor_user_id', 'strategy_id', 'scope', 'expected_risk_revision', 'risk_revision', 'reason', 'created_at_ms']);
  const reason = text(p.reason); const expected = integer(p.expected_risk_revision); const risk = integer(p.risk_revision);
  if (integer(p.strategy_id) !== strategy || p.scope !== 'strategy' || BigInt(risk) !== BigInt(expected) + 1n || !reason || reason.trim() !== reason || reason.includes('\0') || new TextEncoder().encode(reason).length > 256) invalid();
  return { id: integer(p.id), actor_user_id: integer(p.actor_user_id), strategy_id: strategy, scope: 'strategy', expected_risk_revision: expected, risk_revision: risk, reason, created_at_ms: integer(p.created_at_ms) };
}
function record(value: unknown, strategy: string, observed: string): EmergencyRecord {
  const p = fields(value, ['acceptance', 'state', 'revision', 'enrollment_complete'], ['completed_at_ms']);
  const accepted = acceptance(p.acceptance, strategy); const state = text(p.state);
  if (!Object.hasOwn(emergencyStates, state) || BigInt(accepted.created_at_ms) > BigInt(observed)) invalid();
  const complete = boolean(p.enrollment_complete); let completed: string | null = null;
  if (state === 'ACCEPTED') { if (p.completed_at_ms !== undefined) invalid(); }
  else {
    completed = integer(p.completed_at_ms);
    if (!complete || BigInt(completed) < BigInt(accepted.created_at_ms) || BigInt(completed) > BigInt(observed)) invalid();
  }
  return { acceptance: accepted, state: state as EmergencyRecord['state'], revision: integer(p.revision), enrollment_complete: complete, completed_at_ms: completed };
}
export async function requestEmergency(request: EmergencyRequest, signal: AbortSignal): Promise<EmergencyAcceptance> {
  integer(request.strategy_id); integer(request.body.expected_risk_revision);
  const result = acceptance(await managementWriteJSON(`strategies/${request.strategy_id}/emergency-cancellations`, request.key, request.body, signal), request.strategy_id);
  if (result.scope !== request.body.scope || result.expected_risk_revision !== request.body.expected_risk_revision || result.reason !== request.body.reason) invalid();
  return result;
}
export async function getEmergency(strategy: string, id: string, signal: AbortSignal): Promise<EmergencyObservation> {
  integer(strategy); integer(id);
  const p = fields(await managementJSON(`strategies/${strategy}/emergency-cancellations/${id}`, signal), ['record', 'progress', 'observed_at_ms']);
  const observed = integer(p.observed_at_ms); const stored = record(p.record, strategy, observed);
  const raw = fields(p.progress, ['targets', 'terminal', 'failed', 'pending_submissions', 'enrollment_complete']);
  const progress = { targets: integer(raw.targets, true), terminal: integer(raw.terminal, true), failed: integer(raw.failed, true), pending_submissions: integer(raw.pending_submissions, true), enrollment_complete: boolean(raw.enrollment_complete) };
  const targets = BigInt(progress.targets); const terminal = BigInt(progress.terminal); const failed = BigInt(progress.failed);
  if (stored.acceptance.id !== id || progress.enrollment_complete !== stored.enrollment_complete || terminal > targets || failed > targets - terminal) invalid();
  return { record: stored, progress, observed_at_ms: observed };
}
export async function listEmergencies(strategy: string, after: string, limit: number, signal: AbortSignal): Promise<EmergencyHistory> {
  integer(strategy); integer(after, true); if (!Number.isInteger(limit) || limit < 1 || limit > 100) invalid();
  const p = fields(await managementJSON(`strategies/${strategy}/emergency-cancellations?after_id=${after}&limit=${limit}`, signal), ['items', 'next_after_id', 'observed_at_ms']);
  const observed = integer(p.observed_at_ms); if (!Array.isArray(p.items) || p.items.length > limit) invalid();
  let previous = after;
  const items = p.items.map(value => { const result = record(value, strategy, observed); if (BigInt(result.acceptance.id) <= BigInt(previous)) invalid(); previous = result.acceptance.id; return result; });
  const next = integer(p.next_after_id, true); if (next !== '0' && (items.length !== limit || next !== previous)) invalid();
  return { items, next_after_id: next, observed_at_ms: observed };
}
