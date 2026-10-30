import { ApiError, managementJSON } from './api';
import { integer, object, sources, text } from './strategy-api';
import type { Source } from './strategy-api';

export interface Trade {
  id: string; execution_venue: string; trade_id: string; strategy_id: string; account_id: string;
  instrument_id: string; config_version: string; market_data_source: Source; core_order_id: string;
  client_order_id: string; order_id: string | null; side: string; price: string; quantity: string;
  quote_notional: string; fee_amount: string; fee_asset: string; recorded_at_ms: string;
  portfolio_fact: { id: string; recorded_at_ms: string } | null;
}
export interface TradeFilters { orderID: string; side: string; limit: number }
export interface TradePage { trades: Trade[]; next_after_id: string }
function invalid(): never { throw new ApiError('INVALID_RESPONSE'); }
function bounded(value: unknown, max: number): string {
  const result = text(value);
  if (!result || result.trim() !== result || result.includes('\0') || new TextEncoder().encode(result).length > max) invalid();
  return result;
}
function amount(value: unknown, ledger = false, zero = false): string {
  const result = text(value);
  const pattern = ledger ? /^(0|[1-9][0-9]{0,39})(\.[0-9]{1,36})?$/ : /^(0|[1-9][0-9]{0,19})(\.[0-9]{1,18})?$/;
  return pattern.test(result) && (zero || !/^0(?:\.0+)?$/.test(result)) ? result : invalid();
}
// These bounded integer operations validate textual decimal equality only;
// displayed financial strings are never rounded or converted to Number.
function coefficient(value: string): bigint { const [whole, fraction = ''] = value.split('.'); return BigInt(whole + fraction.padEnd(36, '0')); }
function tradeValue(input: unknown, strategy: string): Trade {
  const p = object(input); const source = text(p.market_data_source);
  if (integer(p.strategy_id) !== strategy || !Object.hasOwn(sources, source) || p.execution_venue !== 'simulator' || !['BUY', 'SELL'].includes(text(p.side))) invalid();
  const value: Trade = {
    id: integer(p.id), execution_venue: 'simulator', trade_id: bounded(p.trade_id, 128), strategy_id: strategy, account_id: bounded(p.account_id, 64),
    instrument_id: integer(p.instrument_id), config_version: integer(p.config_version), market_data_source: source as Source, core_order_id: integer(p.core_order_id),
    client_order_id: bounded(p.client_order_id, 64), order_id: Object.hasOwn(p, 'order_id') ? integer(p.order_id) : null, side: text(p.side),
    price: amount(p.price), quantity: amount(p.quantity), quote_notional: amount(p.quote_notional, true), fee_amount: amount(p.fee_amount, true, true),
    fee_asset: bounded(p.fee_asset, 32), recorded_at_ms: integer(p.recorded_at_ms), portfolio_fact: null,
  };
  if (coefficient(value.price) * coefficient(value.quantity) !== coefficient(value.quote_notional) * 10n ** 36n || coefficient(value.fee_amount) >= coefficient(value.quote_notional)) invalid();
  if (p.portfolio_fact !== null) {
    const fact = object(p.portfolio_fact);
    value.portfolio_fact = { id: integer(fact.id), recorded_at_ms: integer(fact.recorded_at_ms) };
    if (value.order_id === null) invalid();
  }
  return value;
}
export async function listTrades(strategy: string, after: string, filters: TradeFilters, signal: AbortSignal): Promise<TradePage> {
  integer(strategy); integer(after, true);
  if (filters.orderID) integer(filters.orderID);
  if (!Number.isInteger(filters.limit) || filters.limit < 1 || filters.limit > 100 || !['', 'BUY', 'SELL'].includes(filters.side)) invalid();
  const query = new URLSearchParams({ limit: String(filters.limit), after_id: after });
  if (filters.orderID) query.set('order_id', filters.orderID);
  if (filters.side) query.set('side', filters.side);
  const page = object(await managementJSON(`strategies/${strategy}/trades?${query}`, signal));
  if (!Array.isArray(page.trades) || page.trades.length > filters.limit) invalid();
  const trades = page.trades.map(value => tradeValue(value, strategy)); const next = integer(page.next_after_id, true); let previous = after;
  for (const trade of trades) {
    if (BigInt(trade.id) <= BigInt(previous) || filters.orderID && trade.order_id !== filters.orderID || filters.side && trade.side !== filters.side) invalid();
    previous = trade.id;
  }
  if (next !== '0' && (trades.length !== filters.limit || next !== previous)) invalid();
  return { trades, next_after_id: next };
}
export async function getTrade(strategy: string, id: string, signal: AbortSignal): Promise<Trade> {
  integer(strategy); integer(id);
  const reply = object(await managementJSON(`strategies/${strategy}/trades/${id}`, signal)); const trade = tradeValue(reply.trade, strategy);
  return trade.id === id ? trade : invalid();
}
