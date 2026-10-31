import { ApiError, managementJSON } from './api';
import { integer, object, text } from './strategy-api';

export interface StoredProjection {
  opening_quantity: string; opening_cash_quote: string; opening_cost_quote: string; quantity: string;
  cash_quote: string; cost_quote: string; realized_pnl_quote: string; fees_quote: string;
  last_source_sequence: string; stored_reconciled: boolean; revision: string; error_code: string;
}
export interface PortfolioProgress { count: string; latest_source_sequence: string }
export interface PortfolioBalance { id: string; asset: string; available: string; frozen: string; revision: string; updated_at_ms: string }
export interface Portfolio {
  id: string; execution_venue: string; symbol: string; account_id: string; instrument_id: string;
  base_asset: string; quote_asset: string; account_enabled: boolean; account_revision: string;
  projection: StoredProjection; source: PortfolioProgress; collected: PortfolioProgress;
  core_base_balance: PortfolioBalance | null; core_quote_balance: PortfolioBalance | null;
  created_at_ms: string; updated_at_ms: string;
}
export interface PortfolioFilters { account: string; instrument: string; limit: number }
export interface PortfolioPage { portfolios: Portfolio[]; next_after_id: string }
function invalid(): never { throw new ApiError('INVALID_RESPONSE'); }
function bounded(value: unknown, maximum: number): string {
  const result = text(value);
  return result && result.trim() === result && !result.includes('\0') && new TextEncoder().encode(result).length <= maximum ? result : invalid();
}
export function portfolioAccount(value: string): string { return /^[A-Za-z0-9_.:-]{1,64}$/.test(value) ? value : invalid(); }
function money(value: unknown, signed = false): string {
  const result = text(value);
  const pattern = signed ? /^-?(0|[1-9][0-9]{0,39})(\.[0-9]{1,36})?$/ : /^(0|[1-9][0-9]{0,39})(\.[0-9]{1,36})?$/;
  return pattern.test(result) ? result : invalid();
}
function bool(value: unknown): boolean { return typeof value === 'boolean' ? value : invalid(); }
function progress(input: unknown): PortfolioProgress {
  const p = object(input); const value = { count: integer(p.count, true), latest_source_sequence: integer(p.latest_source_sequence, true) };
  if ((value.count === '0') !== (value.latest_source_sequence === '0')) invalid();
  return value;
}
function balance(input: unknown, asset: string): PortfolioBalance | null {
  if (input === null) return null;
  const p = object(input);
  if (p.asset !== asset) invalid();
  return { id: integer(p.id), asset, available: money(p.available), frozen: money(p.frozen), revision: integer(p.revision), updated_at_ms: integer(p.updated_at_ms) };
}
function projection(input: unknown): StoredProjection {
  const p = object(input); const code = text(p.error_code);
  if (!['', 'SOURCE_INCOMPLETE', 'RECONCILIATION_MISMATCH'].includes(code)) invalid();
  const value = {
    opening_quantity: money(p.opening_quantity), opening_cash_quote: money(p.opening_cash_quote), opening_cost_quote: money(p.opening_cost_quote),
    quantity: money(p.quantity), cash_quote: money(p.cash_quote), cost_quote: money(p.cost_quote), realized_pnl_quote: money(p.realized_pnl_quote, true), fees_quote: money(p.fees_quote),
    last_source_sequence: integer(p.last_source_sequence, true), stored_reconciled: bool(p.stored_reconciled), revision: integer(p.revision), error_code: code,
  };
  if (/^0(?:\.0+)?$/.test(value.opening_quantity) && !/^0(?:\.0+)?$/.test(value.opening_cost_quote)) invalid();
  return value;
}
function portfolioValue(input: unknown): Portfolio {
  const p = object(input); const base = bounded(p.base_asset, 32); const quote = bounded(p.quote_asset, 32);
  if (p.execution_venue !== 'simulator' || base === quote) invalid();
  return {
    id: integer(p.id), execution_venue: 'simulator', symbol: bounded(p.symbol, 64), account_id: portfolioAccount(text(p.account_id)), instrument_id: integer(p.instrument_id),
    base_asset: base, quote_asset: quote, account_enabled: bool(p.account_enabled), account_revision: integer(p.account_revision),
    projection: projection(p.projection), source: progress(p.source), collected: progress(p.collected),
    core_base_balance: balance(p.core_base_balance, base), core_quote_balance: balance(p.core_quote_balance, quote),
    created_at_ms: integer(p.created_at_ms), updated_at_ms: integer(p.updated_at_ms),
  };
}
export async function listPortfolios(after: string, filters: PortfolioFilters, signal: AbortSignal): Promise<PortfolioPage> {
  integer(after, true); if (filters.account) portfolioAccount(filters.account); if (filters.instrument) integer(filters.instrument);
  if (!Number.isInteger(filters.limit) || filters.limit < 1 || filters.limit > 100) invalid();
  const query = new URLSearchParams({ after_id: after, limit: String(filters.limit) });
  if (filters.account) query.set('account_id', filters.account);
  if (filters.instrument) query.set('instrument_id', filters.instrument);
  const p = object(await managementJSON(`portfolios?${query}`, signal));
  if (!Array.isArray(p.portfolios) || p.portfolios.length > filters.limit) invalid();
  const portfolios = p.portfolios.map(portfolioValue); const next = integer(p.next_after_id, true); let previous = after;
  for (const portfolio of portfolios) {
    if (BigInt(portfolio.id) <= BigInt(previous) || filters.account && portfolio.account_id !== filters.account || filters.instrument && portfolio.instrument_id !== filters.instrument) invalid();
    previous = portfolio.id;
  }
  if (next !== '0' && (portfolios.length !== filters.limit || next !== previous)) invalid();
  return { portfolios, next_after_id: next };
}
export async function getPortfolio(id: string, signal: AbortSignal): Promise<Portfolio> {
  integer(id);
  const p = object(await managementJSON(`portfolios/${id}`, signal)); const value = portfolioValue(p.portfolio);
  return value.id === id ? value : invalid();
}
