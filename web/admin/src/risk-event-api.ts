import { ApiError, managementJSON } from './api';
import { integer, object, text } from './strategy-api';

export const riskEventKinds = ['BASELINE', 'CHANGED', 'REMOVED'] as const;
export const riskEventReasons = ['NOT_EVALUATED', 'READY', 'STRATEGY_STOPPED', 'INVALID_PARAMETERS', 'BUDGET_DISABLED', 'UNRECONCILED', 'MARKET_UNAVAILABLE', 'CONFIGURATION_PENDING'] as const;
export interface RiskEvent {
  id: string; recorded_at_ms: string; kind: typeof riskEventKinds[number]; snapshot_id: string; snapshot_revision: string;
  strategy_id: string; account_id: string; instrument_id: string; config_version: string; market_data_source: 'binance' | 'okx';
  market_feed_id: string | null; publisher_epoch: string; market_sequence: string; publication_revision: string; source_version: string;
  market_maximum_age_ms: string; observed_at_ms: string; expires_at_ms: string; recorded_reconciled: boolean;
  recorded_allow_new_risk: boolean; reason_code: typeof riskEventReasons[number]; snapshot_deleted: boolean;
}
export interface RiskEventFilters { strategy: string; account: string; instrument: string; source: string; kind: string; reason: string; limit: number }
export interface RiskEventPage { events: RiskEvent[]; next_after_id: string }
function invalid(): never { throw new ApiError('INVALID_RESPONSE'); }
export function riskEventAccount(value: string): string { return /^[A-Za-z0-9_.:-]{1,64}$/.test(value) ? value : invalid(); }
function bool(value: unknown): boolean { return typeof value === 'boolean' ? value : invalid(); }
function choice<T extends string>(value: unknown, choices: readonly T[]): T { const result = text(value); return choices.includes(result as T) ? result as T : invalid(); }
export function validateRiskEventFilters(value: RiskEventFilters) {
  if (value.strategy) integer(value.strategy); if (value.account) riskEventAccount(value.account); if (value.instrument) integer(value.instrument);
  if (value.source) choice(value.source, ['binance', 'okx']); if (value.kind) choice(value.kind, riskEventKinds); if (value.reason) choice(value.reason, riskEventReasons);
  if (![1, 20, 50].includes(value.limit)) invalid();
}
function eventValue(input: unknown): RiskEvent {
  const p = object(input);
  const value: RiskEvent = {
    id: integer(p.id), recorded_at_ms: integer(p.recorded_at_ms), kind: choice(p.kind, riskEventKinds), snapshot_id: integer(p.snapshot_id), snapshot_revision: integer(p.snapshot_revision),
    strategy_id: integer(p.strategy_id), account_id: riskEventAccount(text(p.account_id)), instrument_id: integer(p.instrument_id), config_version: integer(p.config_version), market_data_source: choice(p.market_data_source, ['binance', 'okx']),
    market_feed_id: Object.hasOwn(p, 'market_feed_id') ? integer(p.market_feed_id) : null, publisher_epoch: integer(p.publisher_epoch, true), market_sequence: integer(p.market_sequence, true),
    publication_revision: integer(p.publication_revision, true), source_version: integer(p.source_version, true), market_maximum_age_ms: integer(p.market_maximum_age_ms),
    observed_at_ms: integer(p.observed_at_ms), expires_at_ms: integer(p.expires_at_ms), recorded_reconciled: bool(p.recorded_reconciled), recorded_allow_new_risk: bool(p.recorded_allow_new_risk),
    reason_code: choice(p.reason_code, riskEventReasons), snapshot_deleted: bool(p.snapshot_deleted),
  };
  const expiry = BigInt(value.expires_at_ms) - BigInt(value.observed_at_ms);
  if (BigInt(value.market_maximum_age_ms) > 5000n || expiry <= 0n || expiry > 5000n || value.kind === 'REMOVED' && !value.snapshot_deleted) invalid();
  return value;
}
export function riskEventSearch(filters: RiskEventFilters): URLSearchParams {
  validateRiskEventFilters(filters); const query = new URLSearchParams();
  if (filters.strategy) query.set('strategy_id', filters.strategy); if (filters.account) query.set('account_id', filters.account); if (filters.instrument) query.set('instrument_id', filters.instrument);
  if (filters.source) query.set('market_data_source', filters.source); if (filters.kind) query.set('kind', filters.kind); if (filters.reason) query.set('reason_code', filters.reason);
  if (filters.limit !== 20) query.set('limit', String(filters.limit)); return query;
}
export async function listRiskEvents(after: string, filters: RiskEventFilters, signal: AbortSignal): Promise<RiskEventPage> {
  integer(after, true); const query = riskEventSearch(filters); query.set('after_id', after); query.set('limit', String(filters.limit));
  const p = object(await managementJSON(`risk-events?${query}`, signal));
  if (!Array.isArray(p.events) || p.events.length > filters.limit) invalid();
  const events = p.events.map(eventValue); const next = integer(p.next_after_id, true); let previous = after;
  for (const event of events) {
    if (BigInt(event.id) <= BigInt(previous) || filters.strategy && event.strategy_id !== filters.strategy || filters.account && event.account_id !== filters.account ||
      filters.instrument && event.instrument_id !== filters.instrument || filters.source && event.market_data_source !== filters.source || filters.kind && event.kind !== filters.kind || filters.reason && event.reason_code !== filters.reason) invalid();
    previous = event.id;
  }
  if (next !== '0' && (events.length !== filters.limit || next !== previous)) invalid();
  return { events, next_after_id: next };
}
export async function getRiskEvent(id: string, signal: AbortSignal): Promise<RiskEvent> {
  integer(id); const p = object(await managementJSON(`risk-events/${id}`, signal)); const value = eventValue(p.event);
  return value.id === id ? value : invalid();
}
