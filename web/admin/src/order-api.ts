import { ApiError, managementJSON } from './api';
import { integer, object, sources, text } from './strategy-api';
import type { Source } from './strategy-api';

export const orderStates = ['INTENT', 'SUBMITTING', 'LIVE', 'PARTIALLY_FILLED', 'FILLED', 'CANCELED', 'REJECTED', 'UNKNOWN'] as const;
export type OrderState = typeof orderStates[number];
const cancelStates = ['NONE', 'REQUESTED', 'PENDING', 'CONFIRMED', 'UNKNOWN', 'REJECTED'];
const errorCodes = ['', 'OUTCOME_UNKNOWN', 'CORE_NOT_FOUND', 'CORE_REJECTED', 'CORE_NOT_ACCEPTED', 'RISK_DENIED'];
export interface OrderCore {
  id: string; state: string; cancel_state: string; filled_quantity: string; frozen_asset: string;
  frozen_amount: string; quote_fee_rate: string; revision: string; updated_at_ms: string;
}
export interface Order {
  id: string; client_order_id: string; strategy_id: string; account_id: string; instrument_id: string;
  config_version: string; market_data_source: Source; side: string; price: string; quantity: string;
  filled_quantity: string; reserved_notional: string; time_in_force: string; reduce_only: boolean;
  state: OrderState; cancel_state: string; confirmed_core_id: string; confirmed_core_revision: string;
  revision: string; error_code: string; deadline_ms: string; created_at_ms: string; updated_at_ms: string; core: OrderCore | null;
}
export interface OrderFilters { state: string; openOnly: boolean; limit: number }
export interface OrderPage { orders: Order[]; next_after_id: string }
function invalid(): never { throw new ApiError('INVALID_RESPONSE'); }
function bounded(value: unknown, max: number): string {
  const result = text(value);
  if (!result || new TextEncoder().encode(result).length > max || result.trim() !== result || result.includes('\0')) invalid();
  return result;
}
function money(value: unknown, ledger = false, zero = true): string {
  const result = text(value);
  const pattern = ledger ? /^(0|[1-9][0-9]{0,39})(\.[0-9]{1,36})?$/ : /^(0|[1-9][0-9]{0,19})(\.[0-9]{1,18})?$/;
  if (!pattern.test(result) || !zero && /^0(?:\.0+)?$/.test(result)) invalid();
  return result;
}
function atMost(left: string, right: string): boolean {
  const [a, af = ''] = left.split('.'); const [b, bf = ''] = right.split('.');
  return a.length !== b.length ? a.length < b.length : a !== b ? a < b : af.padEnd(36, '0') <= bf.padEnd(36, '0');
}
function boolean(value: unknown): boolean { return typeof value === 'boolean' ? value : invalid(); }
function enumText(value: unknown, allowed: readonly string[]): string { const result = text(value); return allowed.includes(result) ? result : invalid(); }
function orderValue(input: unknown, strategy: string): Order {
  const p = object(input); const source = text(p.market_data_source);
  if (integer(p.strategy_id) !== strategy || !Object.hasOwn(sources, source)) invalid();
  const value: Order = {
    id: integer(p.id), client_order_id: bounded(p.client_order_id, 128), strategy_id: strategy, account_id: bounded(p.account_id, 64), instrument_id: integer(p.instrument_id),
    config_version: integer(p.config_version), market_data_source: source as Source, side: enumText(p.side, ['BUY', 'SELL']),
    price: money(p.price, false, false), quantity: money(p.quantity, false, false), filled_quantity: money(p.filled_quantity), reserved_notional: money(p.reserved_notional, true),
    time_in_force: enumText(p.time_in_force, ['GTC', 'IOC', 'POST_ONLY']), reduce_only: boolean(p.reduce_only), state: enumText(p.state, orderStates) as OrderState,
    cancel_state: enumText(p.cancel_state, cancelStates), confirmed_core_id: integer(p.confirmed_core_id, true), confirmed_core_revision: integer(p.confirmed_core_revision, true),
    revision: integer(p.revision), error_code: enumText(p.error_code, errorCodes), deadline_ms: integer(p.deadline_ms), created_at_ms: integer(p.created_at_ms), updated_at_ms: integer(p.updated_at_ms), core: null,
  };
  if (!atMost(value.filled_quantity, value.quantity) || BigInt(value.updated_at_ms) < BigInt(value.created_at_ms) || (value.confirmed_core_id === '0') !== (value.confirmed_core_revision === '0')) invalid();
  if (p.core !== null) {
    const c = object(p.core);
    value.core = {
      id: integer(c.id), state: enumText(c.state, ['LIVE', 'PARTIALLY_FILLED', 'FILLED', 'CANCELED']), cancel_state: enumText(c.cancel_state, cancelStates),
      filled_quantity: money(c.filled_quantity), frozen_asset: bounded(c.frozen_asset, 32), frozen_amount: money(c.frozen_amount, true), quote_fee_rate: money(c.quote_fee_rate), revision: integer(c.revision), updated_at_ms: integer(c.updated_at_ms),
    };
    if (!atMost(value.core.filled_quantity, value.quantity) || !atMost(value.core.quote_fee_rate, '1') || /^1(?:\.0+)?$/.test(value.core.quote_fee_rate) ||
        value.confirmed_core_id !== '0' && value.confirmed_core_id !== value.core.id || BigInt(value.confirmed_core_revision) > BigInt(value.core.revision)) invalid();
  } else if (value.confirmed_core_id !== '0') invalid();
  return value;
}
export async function listOrders(strategy: string, after: string, filters: OrderFilters, signal: AbortSignal): Promise<OrderPage> {
  integer(strategy); integer(after, true);
  if (!Number.isInteger(filters.limit) || filters.limit < 1 || filters.limit > 100 || filters.state !== '' && !orderStates.includes(filters.state as OrderState)) invalid();
  const query = new URLSearchParams({ limit: String(filters.limit), after_id: after, open_only: String(filters.openOnly) });
  if (filters.state) query.set('state', filters.state);
  const p = object(await managementJSON(`strategies/${strategy}/orders?${query}`, signal));
  if (!Array.isArray(p.orders) || p.orders.length > filters.limit) invalid();
  const orders = p.orders.map(value => orderValue(value, strategy)); const next = integer(p.next_after_id, true); let previous = after;
  for (const order of orders) {
    if (BigInt(order.id) <= BigInt(previous) || filters.state && order.state !== filters.state || filters.openOnly && ['FILLED', 'CANCELED', 'REJECTED'].includes(order.state)) invalid();
    previous = order.id;
  }
  if (next !== '0' && (orders.length !== filters.limit || next !== previous)) invalid();
  return { orders, next_after_id: next };
}
export async function getOrder(strategy: string, id: string, signal: AbortSignal): Promise<Order> {
  integer(strategy); integer(id);
  const p = object(await managementJSON(`strategies/${strategy}/orders/${id}`, signal)); const order = orderValue(p.order, strategy);
  return order.id === id ? order : invalid();
}
