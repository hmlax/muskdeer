import { describe, expect, it } from 'vitest';
import { editableNotional, formatNotional } from './notional';
describe('notional display', () => {
  it.each([
    ['100.000000000000000000', '100.00'], ['0', '0.00'],
    ['0.004999999999999999', '0.00'], ['0.005', '0.01'], ['999.999', '1000.00'],
    ['9007199254740993.125000000000000001', '9007199254740993.13'],
    ['-0.004', '0.00'], ['-12.345', '-12.35'],
  ])('formats %s as %s without floating-point conversion', (input, expected) => {
    expect(formatNotional(input)).toBe(expected);
  });
  it.each([undefined, '', 'invalid', '1e3'])('keeps invalid values unknown: %s', (input) => {
    expect(formatNotional(input)).toBe('—');
  });
});

describe('editable notional', () => {
  it.each([
    ['100.000000000000000000', '100.00'],
    ['100.100000000000000000', '100.10'],
    ['100.123400000000000000', '100.1234'],
    ['9007199254740993.000000000000000001', '9007199254740993.000000000000000001'],
  ])('keeps significant decimals from %s as %s', (input, expected) => {
    expect(editableNotional(input)).toBe(expected);
  });
});
