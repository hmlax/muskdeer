import type { MuskdeerInstanceParameters } from '@/api/generated/admin/v1';

export const longGridImplementation = 'long_grid_v1';

// Template selection sends exactly one concrete algorithm with full parameters.
export function selectedParameters(kind: string, parameters: MuskdeerInstanceParameters, implementation?: string): MuskdeerInstanceParameters {
  const execution = parameters.execution;
  if (kind === 'market_maker') return { execution, marketMaker: parameters.marketMaker };
  if (kind === 'grid' && implementation === longGridImplementation) {
    return { execution, longGrid: { ...parameters.longGrid, openOnStart: true, haltAfterStopLoss: true } };
  }
  if (kind === 'grid') return { execution, grid: parameters.grid };
  if (kind === 'wash_trade') return { execution, washTrade: { ...parameters.washTrade, developmentOnly: true } };
  throw new Error('Unknown strategy type');
}
