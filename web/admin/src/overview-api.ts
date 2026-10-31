import { ApiError, managementJSON } from './api';
import { integer, object, sources, states, strategy as strategyValue, text } from './strategy-api';
import type { State, Strategy } from './strategy-api';

export const serviceRoles = ['api', 'market', 'strategy', 'execution', 'portfolio', 'cron'] as const;
export type ServiceRole = typeof serviceRoles[number];
export const probeStatuses = ['READY', 'NOT_READY', 'UNKNOWN', 'PROBE_NOT_CONFIGURED', 'PROBE_FAILED', 'PROBE_TIMEOUT', 'PROBE_DENIED', 'PROBE_CAPACITY'] as const;
export type ProbeStatus = typeof probeStatuses[number];
export interface OverviewFilters { account: string; source: string; limit: number }
export interface ServiceFilters { role: ServiceRole; limit: number }
export interface RuntimeStrategy extends Strategy { runtime_owner_id: string; runtime_owner_epoch: string; created_at_ms: string }
export interface LeaseObservation { epoch: string; owner_recorded: boolean; expires_at_ms: string; unexpired_at_observation: boolean; runtime_matches_lease: boolean }
export interface StrategyObservation { strategy: RuntimeStrategy; lease: LeaseObservation | null }
export interface StrategyOverview { observed_at_ms: string; total: string; requested_running: string; stored_states: { state: State; count: string }[]; items: StrategyObservation[]; next_after_id: string }
export interface ServiceInstance { instance_id: string; declared_version: string; status: ProbeStatus; probed_at_ms: string | null }
export interface ServiceOverview { role: ServiceRole; membership_source: 'LOCAL_API' | 'NACOS_HEALTHY_GRPC'; membership_observed_at_ms: string; total: string; probe_configured: boolean; items: ServiceInstance[]; next_after_instance_id: string }
function invalid(): never { throw new ApiError('INVALID_RESPONSE'); }
function bool(value: unknown): boolean { return typeof value === 'boolean' ? value : invalid(); }
function choice<T extends string>(value: unknown, choices: readonly T[]): T { const v = text(value); return choices.includes(v as T) ? v as T : invalid(); }
function label(value: unknown, maximum = 128): string { const v = text(value); return v.length > 0 && new TextEncoder().encode(v).length <= maximum && v.trim() === v && !/[\u0000\r\n]/.test(v) ? v : invalid(); }
function account(value: unknown): string { const v = text(value); return /^[A-Za-z0-9_.:-]{1,64}$/.test(v) ? v : invalid(); }
function remoteID(value: unknown): string { const v = text(value); return /^[A-Za-z0-9_.-]{1,128}$/.test(v) ? v : invalid(); }
export function validateOverviewFilters(value: OverviewFilters) { if (value.account) account(value.account); if (value.source) choice(value.source, Object.keys(sources)); if (![1, 20, 50].includes(value.limit)) invalid(); }
export function validateServiceFilters(value: ServiceFilters) { choice(value.role, serviceRoles); if (![1, 4].includes(value.limit)) invalid(); }
export function overviewQuery(filters: OverviewFilters): URLSearchParams {
  validateOverviewFilters(filters); const query = new URLSearchParams();
  if (filters.account) query.set('account_id', filters.account); if (filters.source) query.set('requested_market_data_source', filters.source); query.set('limit', String(filters.limit)); return query;
}
function runtimeStrategy(input: unknown): RuntimeStrategy {
  const p = object(input); const s = strategyValue(p);
  if (Object.hasOwn(p, 'active_config_version') !== Object.hasOwn(p, 'active_market_data_source') || Object.hasOwn(p, 'active_config_version') && (p.active_config_version === null || p.active_market_data_source === null)) invalid();
  const owner = text(p.runtime_owner_id); const epoch = integer(p.runtime_owner_epoch, true); const created = integer(p.created_at_ms);
  if ((epoch === '0') !== (owner === '') || Array.from(owner).length > 128 || /\u0000/.test(owner) ||
    BigInt(s.updated_at_ms) < BigInt(created) || s.active_config_version !== null && BigInt(s.active_config_version) > BigInt(s.requested_config_version)) invalid();
  account(s.account_id); if (!s.instance_key || !s.code_version) invalid();
  return { ...s, runtime_owner_id: owner, runtime_owner_epoch: epoch, created_at_ms: created };
}
function leaseValue(input: unknown, s: RuntimeStrategy, observed: string): LeaseObservation | null {
  if (input === null) return null; const p = object(input);
  const value: LeaseObservation = { epoch: integer(p.epoch, true), owner_recorded: bool(p.owner_recorded), expires_at_ms: integer(p.expires_at_ms, true), unexpired_at_observation: bool(p.unexpired_at_observation), runtime_matches_lease: bool(p.runtime_matches_lease) };
  if ((value.epoch !== '0') !== value.owner_recorded || !value.owner_recorded && value.expires_at_ms !== '0' ||
    value.unexpired_at_observation !== (value.owner_recorded && BigInt(value.expires_at_ms) > BigInt(observed)) || value.runtime_matches_lease && (!value.owner_recorded || !s.runtime_owner_id || s.runtime_owner_epoch !== value.epoch)) invalid();
  return value;
}
export async function getStrategyOverview(after: string, filters: OverviewFilters, signal: AbortSignal): Promise<StrategyOverview> {
  integer(after, true); const query = overviewQuery(filters); query.set('after_id', after);
  const p = object(await managementJSON(`overview/strategies?${query}`, signal)); const observed = integer(p.observed_at_ms); const total = integer(p.total, true); const requested = integer(p.requested_running, true); const next = integer(p.next_after_id, true);
  if (!Array.isArray(p.stored_states) || p.stored_states.length > 9 || !Array.isArray(p.items) || p.items.length > filters.limit || BigInt(p.items.length) > BigInt(total) || BigInt(requested) > BigInt(total)) invalid();
  const counts = new Map<State, bigint>(); let sum = 0n; let previousState = '';
  const stored = p.stored_states.map(input => { const v = object(input); const state = choice(v.state, Object.keys(states) as State[]); const count = integer(v.count); if (state <= previousState) invalid(); previousState = state; sum += BigInt(count); counts.set(state, BigInt(count)); return { state, count }; });
  if (sum !== BigInt(total)) invalid(); let previous = after; let requestedInPage = 0n;
  const items = p.items.map(input => {
    const v = object(input); const s = runtimeStrategy(v.strategy);
    if (BigInt(s.id) <= BigInt(previous) || filters.account && s.account_id !== filters.account || filters.source && s.requested_market_data_source !== filters.source || (counts.get(s.state) ?? 0n) < 1n) invalid();
    counts.set(s.state, counts.get(s.state)! - 1n); previous = s.id; if (s.desired_state === 'RUNNING') requestedInPage++;
    return { strategy: s, lease: leaseValue(v.lease, s, observed) };
  });
  if (requestedInPage > BigInt(requested) || BigInt(items.length) - requestedInPage > BigInt(total) - BigInt(requested) ||
    after === '0' && (BigInt(items.length) < BigInt(total) && items.length < filters.limit || (next !== '0') !== (BigInt(total) > BigInt(items.length))) ||
    next !== '0' && (items.length !== filters.limit || next !== previous || BigInt(total) <= BigInt(items.length))) invalid();
  return { observed_at_ms: observed, total, requested_running: requested, stored_states: stored, items, next_after_id: next };
}
export async function getServiceOverview(after: string, filters: ServiceFilters, signal: AbortSignal): Promise<ServiceOverview> {
  validateServiceFilters(filters); if (after) label(after); const query = new URLSearchParams({ role: filters.role, limit: String(filters.limit) }); if (after) query.set('after_instance_id', after);
  const p = object(await managementJSON(`overview/services?${query}`, signal)); const role = choice(p.role, serviceRoles); const source = choice(p.membership_source, ['LOCAL_API', 'NACOS_HEALTHY_GRPC']);
  const observed = integer(p.membership_observed_at_ms); const total = integer(p.total, true); const configured = bool(p.probe_configured); const next = text(p.next_after_instance_id);
  if (role !== filters.role || BigInt(total) > 1024n || role === 'api' && (source !== 'LOCAL_API' || total !== '1' || !configured) || role !== 'api' && source !== 'NACOS_HEALTHY_GRPC' ||
    !Array.isArray(p.items) || p.items.length > filters.limit || BigInt(p.items.length) > BigInt(total)) invalid();
  let previous = after;
  const items = p.items.map(input => {
    const v = object(input); const id = role === 'api' ? label(v.instance_id) : remoteID(v.instance_id); const version = label(v.declared_version); const status = choice(v.status, probeStatuses);
    const probed = Object.hasOwn(v, 'probed_at_ms') ? integer(v.probed_at_ms) : null;
    if (id <= previous || configured && (status === 'PROBE_NOT_CONFIGURED' || probed === null) || !configured && (status !== 'PROBE_NOT_CONFIGURED' || probed !== null) || role === 'api' && !['READY', 'NOT_READY', 'PROBE_TIMEOUT'].includes(status)) invalid();
    previous = id; return { instance_id: id, declared_version: version, status, probed_at_ms: probed };
  });
  if (after === '' && (BigInt(items.length) < BigInt(total) && items.length < filters.limit || (next !== '') !== (BigInt(total) > BigInt(items.length))) ||
    next !== '' && (items.length !== filters.limit || next !== previous || BigInt(total) <= BigInt(items.length))) invalid();
  return { role, membership_source: source, membership_observed_at_ms: observed, total, probe_configured: configured, items, next_after_instance_id: next };
}
export function overviewTime(value: string): string {
  // Only bounded integer UTC milliseconds enter Date; no financial value does.
  if (BigInt(value) > 8640000000000000n) return value;
  return new Date(Number(value)).toISOString();
}

