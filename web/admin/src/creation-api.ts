import { ApiError, managementJSON, managementWriteJSON } from './api';
import { decimal, integer, object, sources, text } from './strategy-api';
import type { GridParameters, MakerParameters, Source } from './strategy-api';

export interface AccountChoice { id: string; account_id: string; enabled: boolean; quote_fee_rate: string }
export interface InstrumentChoice { id: string; symbol: string; status: string; price_tick: string; quantity_step: string; min_notional: string }
export interface FeedChoice { id: string; source: Source }
export interface CatalogPage<T> { items: T[]; next: string }
export interface CreationInput {
  instance_key: string; account_id: string; instrument_id: string; code_version: string;
  market_data_source: Source; market_mode: 'LIVE' | 'REPLAY'; reason: string;
  parameters: { grid: GridParameters } | { market_maker: MakerParameters };
}
export interface CreationReceipt { id: string; strategy_id: string; market_data_source: Source; reason: string }

function page<T extends { id: string }>(value: unknown, field: string, cursor: string, parse: (value: unknown) => T): CatalogPage<T> {
  const p = object(value); const values = p[field];
  if (!Array.isArray(values) || values.length > 100) throw new ApiError('INVALID_RESPONSE');
  const items = values.map(parse); let previous = integer(cursor, true);
  for (const item of items) { if (BigInt(item.id) <= BigInt(previous)) throw new ApiError('INVALID_RESPONSE'); previous = item.id; }
  const next = integer(p.next_after_id, true);
  if (next !== '0' && (items.length === 0 || next !== previous)) throw new ApiError('INVALID_RESPONSE');
  return { items, next };
}
export async function accountChoices(cursor: string, signal: AbortSignal): Promise<CatalogPage<AccountChoice>> {
  integer(cursor, true);
  return page(await managementJSON(`accounts?limit=100&after_id=${cursor}`, signal), 'accounts', cursor, value => {
    const a = object(value); if (typeof a.enabled !== 'boolean') throw new ApiError('INVALID_RESPONSE');
    return { id: integer(a.id), account_id: text(a.account_id), enabled: a.enabled, quote_fee_rate: decimal(a.quote_fee_rate) };
  });
}
export async function instrumentChoices(cursor: string, signal: AbortSignal): Promise<CatalogPage<InstrumentChoice>> {
  integer(cursor, true);
  return page(await managementJSON(`instruments?limit=100&after_id=${cursor}`, signal), 'instruments', cursor, value => {
    const i = object(value); if (i.status !== 'TRADING' && i.status !== 'HALTED') throw new ApiError('INVALID_RESPONSE');
    return { id: integer(i.id), symbol: text(i.symbol), status: i.status, price_tick: decimal(i.price_tick), quantity_step: decimal(i.quantity_step), min_notional: decimal(i.min_notional) };
  });
}
export async function feedChoices(id: string, mode: 'LIVE' | 'REPLAY', signal: AbortSignal): Promise<FeedChoice[]> {
  integer(id);
  const result = page(await managementJSON(`instruments/${id}/market-feeds?mode=${mode}&limit=100`, signal), 'feeds', '0', value => {
    const f = object(value);
    if (integer(f.instrument_id) !== id || f.mode !== mode || typeof f.source !== 'string' || !Object.hasOwn(sources, f.source)) throw new ApiError('INVALID_RESPONSE');
    return { id: integer(f.id), source: f.source as Source };
  });
  if (result.next !== '0' || new Set(result.items.map(f => f.source)).size !== result.items.length) throw new ApiError('INVALID_RESPONSE');
  return result.items;
}
export async function createStrategy(body: CreationInput, key: string, signal: AbortSignal): Promise<CreationReceipt> {
  const p = object(await managementWriteJSON('strategies', key, body, signal));
  if (integer(p.actor_user_id) === '0' || integer(p.config_version) !== '1' || integer(p.revision) !== '1' || p.market_data_source !== body.market_data_source || p.reason !== body.reason) throw new ApiError('INVALID_RESPONSE');
  integer(p.created_at_ms);
  return { id: integer(p.id), strategy_id: integer(p.strategy_id), market_data_source: body.market_data_source, reason: text(p.reason) };
}
