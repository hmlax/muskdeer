import type { GridParameters, MakerParameters, Parameters, Source, Template } from './strategy-api';

export type GridDraft = { [K in keyof GridParameters]: string };
export type MakerDraft = { [K in Exclude<keyof MakerParameters, 'hedge_enabled' | 'hedge_threshold' | 'hedge_max_wait_ms' | 'max_hedge_backlog'>]: string };
export type ParameterInput = { grid: GridParameters } | { market_maker: MakerParameters };
export const initialGrid: GridDraft = { lower_price: '98', upper_price: '102', grid_count: '5', total_budget: '50', per_order_notional: '10', inventory_limit: '20', max_market_age_ms: '500' };
export const initialMaker: MakerDraft = { quote_levels: '2', level_spacing_bps: '2', base_spread_bps: '10', level_notional: '10', side_budget: '20', inventory_target: '10', inventory_soft_limit: '15', inventory_hard_limit: '20', reprice_bps: '1', min_refresh_interval_ms: '100', max_quote_age_ms: '1000', max_market_age_ms: '500' };
export const gridFields: [keyof GridDraft, string][] = [['lower_price', '下界价格'], ['upper_price', '上界价格'], ['grid_count', '网格数量'], ['total_budget', '总预算（报价资产）'], ['per_order_notional', '单笔名义额（报价资产）'], ['inventory_limit', '库存上限（基础资产）'], ['max_market_age_ms', '行情最大年龄（ms）']];
export const makerFields: [keyof MakerDraft, string][] = [['quote_levels', '报价档数'], ['level_spacing_bps', '档距（BPS）'], ['base_spread_bps', '基础价差（BPS）'], ['level_notional', '单档名义额（报价资产）'], ['side_budget', '单侧预算（报价资产）'], ['inventory_target', '目标库存（基础资产）'], ['inventory_soft_limit', '库存软限（基础资产）'], ['inventory_hard_limit', '库存硬限（基础资产）'], ['reprice_bps', '重报阈值（BPS）'], ['min_refresh_interval_ms', '最小刷新间隔（ms）'], ['max_quote_age_ms', '挂单最大年龄（ms）'], ['max_market_age_ms', '行情最大年龄（ms）']];
const decimalInput = /^(0|[1-9][0-9]{0,19})(\.[0-9]{1,18})?$/;

export function validDraft(template: Template, grid: GridDraft, maker: MakerDraft): boolean {
  const fields = template === 'grid' ? gridFields : makerFields; const draft = template === 'grid' ? grid : maker;
  return fields.every(([key]) => { const value = draft[key as keyof typeof draft] as string; return key === 'grid_count' || key === 'quote_levels' ? /^[1-9][0-9]{0,2}$/.test(value) && Number(value) <= (key === 'grid_count' ? 200 : 20) : key.endsWith('_ms') ? /^[1-9][0-9]{0,6}$/.test(value) && BigInt(value) <= 3600000n : decimalInput.test(value); });
}

export function parameterInput(template: Template, grid: GridDraft, maker: MakerDraft): ParameterInput {
  return template === 'grid' ? { grid: { ...grid, grid_count: Number(grid.grid_count) } } : { market_maker: { ...maker, quote_levels: Number(maker.quote_levels), hedge_enabled: false, hedge_threshold: '0', hedge_max_wait_ms: '0', max_hedge_backlog: '0' } };
}

export function parameterDraft(parameters: Parameters): { grid: GridDraft; maker: MakerDraft } {
  if (parameters.kind === 'grid') return { grid: { ...parameters.values, grid_count: String(parameters.values.grid_count) }, maker: initialMaker };
  const p = parameters.values;
  return { grid: initialGrid, maker: { quote_levels: String(p.quote_levels), level_spacing_bps: p.level_spacing_bps, base_spread_bps: p.base_spread_bps, level_notional: p.level_notional, side_budget: p.side_budget, inventory_target: p.inventory_target, inventory_soft_limit: p.inventory_soft_limit, inventory_hard_limit: p.inventory_hard_limit, reprice_bps: p.reprice_bps, min_refresh_interval_ms: p.min_refresh_interval_ms, max_quote_age_ms: p.max_quote_age_ms, max_market_age_ms: p.max_market_age_ms } };
}

export function selectableSource(source: Source | '', feeds: { source: Source }[]): source is Source { return source !== '' && feeds.some(f => f.source === source); }
