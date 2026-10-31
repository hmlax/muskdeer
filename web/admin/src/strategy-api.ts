import { ApiError, managementJSON } from './api';

export const sources = { binance: 'Binance', okx: 'OKX' } as const;
export const templates = { market_maker: '现货做市', grid: '网格' } as const;
export const states = {
  CREATED: '已创建', STARTING: '启动中', RECOVERING: '恢复中', RUNNING: '运行中',
  PAUSING: '暂停中', PAUSED: '已暂停', STOPPING: '停止中', STOPPED: '已停止', ERROR: '错误',
} as const;
export type Source = keyof typeof sources;
export type Template = keyof typeof templates;
export type State = keyof typeof states;

export interface Strategy {
  id: string;
  instance_key: string;
  account_id: string;
  instrument_id: string;
  template: Template;
  code_version: string;
  revision: string;
  state: State;
  desired_state: 'RUNNING' | 'PAUSED' | 'STOPPED';
  requested_config_version: string;
  requested_market_data_source: Source;
  active_config_version: string | null;
  active_market_data_source: Source | null;
  market_mode: 'LIVE' | 'REPLAY';
  updated_at_ms: string;
}

export interface MakerParameters {
  quote_levels: number;
  level_spacing_bps: string;
  base_spread_bps: string;
  level_notional: string;
  side_budget: string;
  inventory_target: string;
  inventory_soft_limit: string;
  inventory_hard_limit: string;
  reprice_bps: string;
  min_refresh_interval_ms: string;
  max_quote_age_ms: string;
  max_market_age_ms: string;
  hedge_enabled: boolean;
  hedge_threshold: string;
  hedge_max_wait_ms: string;
  max_hedge_backlog: string;
}

export interface GridParameters {
  lower_price: string;
  upper_price: string;
  grid_count: number;
  total_budget: string;
  per_order_notional: string;
  inventory_limit: string;
  max_market_age_ms: string;
}

export type Parameters = { kind: 'market_maker'; values: MakerParameters } | { kind: 'grid'; values: GridParameters };
export interface ConfigVersion {
  id: string;
  strategy_id: string;
  version: string;
  market_data_source: Source;
  code_version: string;
  created_by: string;
  parameters_schema_version: string;
  parameters: Parameters | null;
}
export interface StrategyPage { strategies: Strategy[]; next_after_id: string }
export interface VersionPage { versions: ConfigVersion[]; next_after_id: string }
export interface StrategyDetail { strategy: Strategy; requested_config: ConfigVersion; active_config: ConfigVersion | null }

export function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new ApiError('INVALID_RESPONSE');
  return value as Record<string, unknown>;
}
export function text(value: unknown): string {
  if (typeof value !== 'string' || value.length > 256) throw new ApiError('INVALID_RESPONSE');
  return value;
}
export function integer(value: unknown, zero = false): string {
  const input = text(value);
  if (!/^(0|[1-9][0-9]{0,18})$/.test(input) || BigInt(input) > 9223372036854775807n || (!zero && input === '0')) throw new ApiError('INVALID_RESPONSE');
  return input;
}
export function decimal(value: unknown): string {
  const input = text(value);
  if (!/^(0|[1-9][0-9]{0,19})(\.[0-9]{1,18})?$/.test(input)) throw new ApiError('INVALID_RESPONSE');
  return input;
}
function source(value: unknown): Source {
  if (value !== 'binance' && value !== 'okx') throw new ApiError('INVALID_RESPONSE');
  return value;
}
function state(value: unknown): State {
  if (typeof value !== 'string' || !Object.hasOwn(states, value)) throw new ApiError('INVALID_RESPONSE');
  return value as State;
}
function count(value: unknown, maximum: number): number {
  if (typeof value !== 'number' || !Number.isInteger(value) || value < 1 || value > maximum) throw new ApiError('INVALID_RESPONSE');
  return value;
}
function boolean(value: unknown): boolean {
  if (typeof value !== 'boolean') throw new ApiError('INVALID_RESPONSE');
  return value;
}
export function strategy(value: unknown): Strategy {
  const p = object(value);
  if ((p.template !== 'market_maker' && p.template !== 'grid') || !['RUNNING', 'PAUSED', 'STOPPED'].includes(text(p.desired_state)) || (p.market_mode !== 'LIVE' && p.market_mode !== 'REPLAY')) throw new ApiError('INVALID_RESPONSE');
  const activeVersion = p.active_config_version == null ? null : integer(p.active_config_version);
  const activeSource = p.active_market_data_source == null ? null : source(p.active_market_data_source);
  if ((activeVersion === null) !== (activeSource === null)) throw new ApiError('INVALID_RESPONSE');
  return {
    id: integer(p.id), instance_key: text(p.instance_key), account_id: text(p.account_id), instrument_id: integer(p.instrument_id),
    template: p.template, code_version: text(p.code_version), revision: integer(p.revision), state: state(p.state),
    desired_state: p.desired_state as Strategy['desired_state'], requested_config_version: integer(p.requested_config_version),
    requested_market_data_source: source(p.requested_market_data_source), active_config_version: activeVersion, active_market_data_source: activeSource,
    market_mode: p.market_mode, updated_at_ms: integer(p.updated_at_ms),
  };
}
function parameters(value: unknown): Parameters {
  const p = object(value);
  if (p.grid != null && p.market_maker == null) {
    const g = object(p.grid);
    return { kind: 'grid', values: {
      lower_price: decimal(g.lower_price), upper_price: decimal(g.upper_price), grid_count: count(g.grid_count, 200),
      total_budget: decimal(g.total_budget), per_order_notional: decimal(g.per_order_notional), inventory_limit: decimal(g.inventory_limit), max_market_age_ms: integer(g.max_market_age_ms),
    } };
  }
  if (p.market_maker == null || p.grid != null) throw new ApiError('INVALID_RESPONSE');
  const m = object(p.market_maker);
  return { kind: 'market_maker', values: {
    quote_levels: count(m.quote_levels, 20), level_spacing_bps: decimal(m.level_spacing_bps), base_spread_bps: decimal(m.base_spread_bps),
    level_notional: decimal(m.level_notional), side_budget: decimal(m.side_budget), inventory_target: decimal(m.inventory_target),
    inventory_soft_limit: decimal(m.inventory_soft_limit), inventory_hard_limit: decimal(m.inventory_hard_limit), reprice_bps: decimal(m.reprice_bps),
    min_refresh_interval_ms: integer(m.min_refresh_interval_ms), max_quote_age_ms: integer(m.max_quote_age_ms), max_market_age_ms: integer(m.max_market_age_ms),
    hedge_enabled: boolean(m.hedge_enabled), hedge_threshold: decimal(m.hedge_threshold), hedge_max_wait_ms: integer(m.hedge_max_wait_ms, true), max_hedge_backlog: decimal(m.max_hedge_backlog),
  } };
}
function config(value: unknown): ConfigVersion {
  const p = object(value);
  const schema = integer(p.parameters_schema_version, true);
  if (schema !== '0' && schema !== '1' || schema === '0' && p.parameters != null) throw new ApiError('INVALID_RESPONSE');
  return {
    id: integer(p.id), strategy_id: integer(p.strategy_id), version: integer(p.version), market_data_source: source(p.market_data_source),
    code_version: text(p.code_version), created_by: text(p.created_by), parameters_schema_version: schema,
    parameters: schema === '0' ? null : parameters(p.parameters),
  };
}
function items(value: unknown): unknown[] {
  if (!Array.isArray(value) || value.length > 100) throw new ApiError('INVALID_RESPONSE');
  return value;
}

export async function listStrategies(after: string, signal: AbortSignal): Promise<StrategyPage> {
  integer(after, true);
  const p = object(await managementJSON(`strategies?limit=20&after_id=${after}`, signal));
  return { strategies: items(p.strategies).map(strategy), next_after_id: integer(p.next_after_id, true) };
}
export async function getStrategy(id: string, signal: AbortSignal): Promise<StrategyDetail> {
  integer(id);
  const p = object(await managementJSON(`strategies/${id}`, signal));
  const result = { strategy: strategy(p.strategy), requested_config: config(p.requested_config), active_config: p.active_config == null ? null : config(p.active_config) };
  if (result.strategy.id !== id || result.requested_config.strategy_id !== id || result.requested_config.version !== result.strategy.requested_config_version || result.requested_config.market_data_source !== result.strategy.requested_market_data_source || result.requested_config.parameters && result.requested_config.parameters.kind !== result.strategy.template ||
      (result.active_config === null) !== (result.strategy.active_config_version === null) || result.active_config && (result.active_config.strategy_id !== id || result.active_config.version !== result.strategy.active_config_version || result.active_config.market_data_source !== result.strategy.active_market_data_source)) throw new ApiError('INVALID_RESPONSE');
  return result;
}
export async function listVersions(id: string, after: string, signal: AbortSignal): Promise<VersionPage> {
  integer(id); integer(after, true);
  const p = object(await managementJSON(`strategies/${id}/versions?limit=20&after_id=${after}`, signal));
  const versions = items(p.versions).map(config);
  if (versions.some(v => v.strategy_id !== id)) throw new ApiError('INVALID_RESPONSE');
  return { versions, next_after_id: integer(p.next_after_id, true) };
}
