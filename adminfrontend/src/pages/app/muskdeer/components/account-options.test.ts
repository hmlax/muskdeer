import { describe, expect, it, vi } from 'vitest';
import { loadAccountOptions, validTradingID } from './account-options';

describe('trading account registration inputs', () => {
  it('preserves the full positive int64 identifier range', () => {
    expect(validTradingID('9223372036854775807')).toBe(true);
    expect(validTradingID('9223372036854775808')).toBe(false);
    expect(validTradingID('0')).toBe(false);
    expect(validTradingID('1.5')).toBe(false);
    expect(validTradingID('1e4')).toBe(false);
    expect(validTradingID('010')).toBe(false);
  });

  it('continues past the first server page without truncating selectors', async () => {
    const read = vi.fn().mockResolvedValueOnce({ items: ['first', 'second'], total: 3 })
      .mockResolvedValueOnce({ items: ['last'], total: 3 });
    expect(await loadAccountOptions(read)).toEqual(['first', 'second', 'last']);
    expect(read).toHaveBeenNthCalledWith(2, { page: 2, pageSize: 200 });
  });

  it('rejects a stalled page instead of silently returning a partial account list', async () => {
    await expect(loadAccountOptions(vi.fn().mockResolvedValue({ items: [], total: 1 }))).rejects.toThrow('incomplete');
  });
});
