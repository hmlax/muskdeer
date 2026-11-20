import { describe, expect, it } from 'vitest';
import type { MuskdeerInstanceParameters } from '@/api/generated/admin/v1';
import { longGridImplementation, selectedParameters } from './instance-parameters';

describe('strategy template parameter selection', () => {
  const input: MuskdeerInstanceParameters = {
    execution: { leverage: 10 },
    grid: { lowerPrice: '90', upperPrice: '110', perOrderNotional: '100' },
    longGrid: { spacingFraction: '0.01', stopLossFraction: '0.005', orderNotional: '100', maximumPositionNotional: '100', openOnStart: true, haltAfterStopLoss: true },
    washTrade: { developmentOnly: true },
  };
  it('keeps long-only entry/stop values and excludes other algorithms', () => {
    expect(selectedParameters('grid', input, longGridImplementation)).toEqual({ execution: { leverage: 10 }, longGrid: input.longGrid });
  });
  it('switches back to the fixed grid without leaking long-only defaults', () => {
    expect(selectedParameters('grid', input, 'price_grid_v1')).toEqual({ execution: { leverage: 10 }, grid: input.grid });
  });
});
