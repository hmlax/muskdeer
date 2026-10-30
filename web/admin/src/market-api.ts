import { ApiError, managementJSON } from './api';
import { integer, object, sources, text } from './strategy-api';
import type { Source } from './strategy-api';

export const marketQualities = { HEALTHY: '健康', UNAVAILABLE: '不可用', WARMING: '恢复观察中', INVALID: '无效', STALE: '过期', OUTLIER: '价格离群', WIDE_SPREAD: '价差过大', TIME_SKEW: '时钟异常' } as const;
export type MarketQuality = keyof typeof marketQualities;
export type MarketMode = 'LIVE' | 'REPLAY';
export interface MarketFilters { mode: MarketMode; source: Source | ''; instrument: string; limit: number }
export const defaultMarketFilters: MarketFilters = { mode: 'LIVE', source: '', instrument: '', limit: 20 };
const qualityReasons: Record<MarketQuality, readonly string[]> = {
  HEALTHY: [''], UNAVAILABLE: ['SYNCHRONIZING', 'DISCONNECTED'], WARMING: ['RECOVERY_SAMPLES'], INVALID: ['SEQUENCE', 'PAYLOAD', 'OVERFLOW'],
  STALE: ['AGE'], OUTLIER: ['PRICE_JUMP'], WIDE_SPREAD: ['SPREAD'], TIME_SKEW: ['FUTURE_TIME', 'CLOCK_REVERSED'],
};
export interface MarketPolicy {
  maximum_age_ms: string; maximum_skew_ms: string; maximum_spread_bps: string; maximum_jump_bps: string; recovery_samples: number; recovery_window_ms: string;
}
export interface MarketFeed { id: string; source: Source; instrument_id: string; external_symbol: string; mode: MarketMode; source_version: string; policy: MarketPolicy }
export interface MarketInstrument { id: string; execution_venue: string; symbol: string; base_asset: string; quote_asset: string; price_tick: string; quantity_step: string; min_notional: string; status: string; metadata_version: string }
export interface MarketReading {
  connection: string; generation: string; sequence: string; quality: MarketQuality; reason: string; bid_price: string; bid_quantity: string; ask_price: string; ask_quantity: string;
  mid_price: string; event_time_ms: string; received_at_ms: string; evaluated_at_ms: string; valid_until_ms: string;
}
export interface MarketSnapshot { revision: string; publisher_epoch: string; publisher_current: boolean; publisher_expires_at_ms: string; published_at_ms: string; published_valid_until_ms: string; published_quality: MarketQuality; published_reason: string; reading: MarketReading }
export interface Market { feed: MarketFeed; instrument: MarketInstrument; evaluated_at_ms: string; snapshot: MarketSnapshot | null; started: number }
export interface MarketPage { markets: Market[]; next_after_id: string }
function invalid(): never { throw new ApiError('INVALID_RESPONSE'); }
function bounded(value: unknown, max: number): string { const s = text(value); return s && s.trim() === s && !s.includes('\0') && new TextEncoder().encode(s).length <= max ? s : invalid(); }
function boolean(value: unknown): boolean { return typeof value === 'boolean' ? value : invalid(); }
function mode(value: unknown): MarketMode { return value === 'LIVE' || value === 'REPLAY' ? value : invalid(); }
function source(value: unknown): Source { return typeof value === 'string' && Object.hasOwn(sources, value) ? value as Source : invalid(); }
function zero(value: string): boolean { return /^0(?:\.0+)?$/.test(value); }
function money(value: unknown, wide = false, allowZero = true): string {
  const s = text(value); const pattern = wide ? /^(0|[1-9][0-9]{0,39})(\.[0-9]{1,36})?$/ : /^(0|[1-9][0-9]{0,19})(\.[0-9]{1,18})?$/;
  return pattern.test(s) && (allowZero || !zero(s)) ? s : invalid();
}
// Compare bounded decimal text for shape checks; prices never become Number.
function less(a: string, b: string): boolean {
  const [ai, af = ''] = a.split('.'); const [bi, bf = ''] = b.split('.');
  return ai.length !== bi.length ? ai.length < bi.length : ai !== bi ? ai < bi : af.padEnd(36, '0') < bf.padEnd(36, '0');
}
function quality(value: unknown, reason: unknown): MarketQuality {
  const q = text(value); const r = text(reason);
  return Object.hasOwn(qualityReasons, q) && qualityReasons[q as MarketQuality].includes(r) ? q as MarketQuality : invalid();
}
function policy(value: unknown): MarketPolicy {
  const p = object(value); const result = {
    maximum_age_ms: integer(p.maximum_age_ms), maximum_skew_ms: integer(p.maximum_skew_ms, true), maximum_spread_bps: money(p.maximum_spread_bps, false, false), maximum_jump_bps: money(p.maximum_jump_bps, false, false),
    recovery_samples: p.recovery_samples, recovery_window_ms: integer(p.recovery_window_ms, true),
  };
  if (BigInt(result.maximum_age_ms) > 5000n || BigInt(result.maximum_skew_ms) > 1000n || BigInt(result.recovery_window_ms) >= BigInt(result.maximum_age_ms) ||
      less('10000', result.maximum_spread_bps) || less('10000', result.maximum_jump_bps) || typeof result.recovery_samples !== 'number' || !Number.isInteger(result.recovery_samples) || result.recovery_samples < 2 || result.recovery_samples > 20) invalid();
  return { ...result, recovery_samples: result.recovery_samples as number };
}
function marketValue(value: unknown, started: number): Market {
  const p = object(value); const f = object(p.feed); const i = object(p.instrument);
  const feed: MarketFeed = { id: integer(f.id), source: source(f.source), instrument_id: integer(f.instrument_id), external_symbol: bounded(f.external_symbol, 64), mode: mode(f.mode), source_version: integer(f.source_version), policy: policy(f.policy) };
  const instrument: MarketInstrument = {
    id: integer(i.id), execution_venue: bounded(i.execution_venue, 32), symbol: bounded(i.symbol, 64), base_asset: bounded(i.base_asset, 32), quote_asset: bounded(i.quote_asset, 32),
    price_tick: money(i.price_tick, false, false), quantity_step: money(i.quantity_step, false, false), min_notional: money(i.min_notional, false, false), status: text(i.status), metadata_version: integer(i.metadata_version),
  };
  if (feed.instrument_id !== instrument.id || feed.source_version !== '1' || !['TRADING', 'HALTED'].includes(instrument.status) || feed.external_symbol !== (feed.source === 'okx' ? `${instrument.base_asset}-${instrument.quote_asset}` : instrument.base_asset + instrument.quote_asset)) invalid();
  const result: Market = { feed, instrument, evaluated_at_ms: integer(p.evaluated_at_ms), snapshot: null, started };
  if (p.snapshot !== null) result.snapshot = snapshotValue(p.snapshot, result);
  return result;
}
function snapshotValue(value: unknown, market: Market): MarketSnapshot {
  const s = object(value); const r = object(s.reading);
  const reading: MarketReading = {
    connection: integer(r.connection, true), generation: integer(r.generation, true), sequence: integer(r.sequence, true), quality: quality(r.quality, r.reason), reason: text(r.reason),
    bid_price: money(r.bid_price), bid_quantity: money(r.bid_quantity), ask_price: money(r.ask_price), ask_quantity: money(r.ask_quantity), mid_price: money(r.mid_price, true),
    event_time_ms: integer(r.event_time_ms, true), received_at_ms: integer(r.received_at_ms, true), evaluated_at_ms: integer(r.evaluated_at_ms), valid_until_ms: integer(r.valid_until_ms, true),
  };
  const result: MarketSnapshot = {
    revision: integer(s.revision), publisher_epoch: integer(s.publisher_epoch), publisher_current: boolean(s.publisher_current), publisher_expires_at_ms: integer(s.publisher_expires_at_ms, true),
    published_at_ms: integer(s.published_at_ms), published_valid_until_ms: integer(s.published_valid_until_ms, true), published_quality: quality(s.published_quality, s.published_reason), published_reason: text(s.published_reason), reading,
  };
  const now = BigInt(market.evaluated_at_ms); const until = BigInt(reading.valid_until_ms); const rule = market.feed.policy;
  if (reading.evaluated_at_ms !== market.evaluated_at_ms || result.publisher_current && BigInt(result.publisher_expires_at_ms) <= now ||
      result.published_quality === 'HEALTHY' && BigInt(result.published_valid_until_ms) <= BigInt(result.published_at_ms)) invalid();
  if (reading.quality === 'HEALTHY') {
    if (!result.publisher_current || result.published_quality !== 'HEALTHY' || reading.connection === '0' || reading.generation === '0' || !less(reading.bid_price, reading.ask_price) ||
        [reading.bid_price, reading.ask_price, reading.mid_price, reading.bid_quantity, reading.ask_quantity].some(zero) || until <= now || until > BigInt(result.publisher_expires_at_ms) || until > BigInt(result.published_valid_until_ms) ||
        [reading.event_time_ms, reading.received_at_ms].some(t => t === '0' || BigInt(t) > now + BigInt(rule.maximum_skew_ms) || now - BigInt(t) >= BigInt(rule.maximum_age_ms) || until > BigInt(t) + BigInt(rule.maximum_age_ms))) invalid();
  } else if (![reading.bid_price, reading.ask_price, reading.mid_price, reading.bid_quantity, reading.ask_quantity].every(zero)) invalid();
  if (!result.publisher_current && (reading.quality !== 'UNAVAILABLE' || reading.reason !== 'DISCONNECTED' || until !== 0n)) invalid();
  return result;
}
export function validMarketFilters(filters: MarketFilters): boolean {
  try { mode(filters.mode); if (filters.source) source(filters.source); if (filters.instrument) integer(filters.instrument); return Number.isInteger(filters.limit) && filters.limit >= 1 && filters.limit <= 100; } catch { return false; }
}
export function marketFilterURL(filters: MarketFilters): URLSearchParams {
  const p = new URLSearchParams({ mode: filters.mode, limit: String(filters.limit) });
  if (filters.source) p.set('source', filters.source); if (filters.instrument) p.set('instrument_id', filters.instrument); return p;
}
export function initialMarketFilters(): MarketFilters | null {
  const p = new URLSearchParams(location.search); const allowed = ['mode', 'source', 'instrument_id', 'limit'];
  if ([...p.keys()].some(k => !allowed.includes(k) || p.getAll(k).length !== 1)) return null;
  const result = { mode: p.get('mode') ?? 'LIVE', source: p.get('source') ?? '', instrument: p.get('instrument_id') ?? '', limit: 20 };
  const limit = p.get('limit'); if (limit !== null) { if (!['1', '20', '50'].includes(limit)) return null; result.limit = Number(limit); }
  return validMarketFilters(result as MarketFilters) ? result as MarketFilters : null;
}
export async function listMarkets(filters: MarketFilters, after: string, signal: AbortSignal): Promise<MarketPage> {
  if (!validMarketFilters(filters)) invalid(); integer(after, true); const query = marketFilterURL(filters); query.set('after_id', after); const started = performance.now();
  const p = object(await managementJSON(`markets?${query}`, signal)); if (!Array.isArray(p.markets) || p.markets.length > filters.limit) invalid();
  const markets = p.markets.map(value => marketValue(value, started)); let previous = after;
  for (const market of markets) {
    if (BigInt(market.feed.id) <= BigInt(previous) || market.feed.mode !== filters.mode || filters.source && market.feed.source !== filters.source || filters.instrument && market.instrument.id !== filters.instrument) invalid();
    previous = market.feed.id;
  }
  const next = integer(p.next_after_id, true); if (next !== '0' && (markets.length !== filters.limit || previous !== next)) invalid();
  return { markets, next_after_id: next };
}
export async function getMarket(id: string, signal: AbortSignal): Promise<Market> {
  integer(id); const started = performance.now(); const p = object(await managementJSON(`markets/${id}`, signal)); const market = marketValue(p.market, started);
  return market.feed.id === id ? market : invalid();
}
