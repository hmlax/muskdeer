import { describe, expect, it } from 'vitest';
import { formatTimestampMs, timestampMillis } from './timestamp';

describe('Muskdeer millisecond timestamps', () => {
  it.each([1794268800000, '1794268800000'])(
    'renders protobuf milliseconds %s as local date/time', (value) => {
      expect(formatTimestampMs(value)).toBe(new Date('2026-11-10T00:00:00Z').toLocaleString());
      expect(timestampMillis(value)).toBe(1794268800000);
    },
  );

  it.each([undefined, null, '', 0, '0', -1, 'invalid', '2026-11-10', Infinity, '9999999999999999999'])(
    'uses a placeholder for absent or invalid milliseconds %s', (value) => {
      expect(formatTimestampMs(value)).toBe('—');
      expect(timestampMillis(value)).toBeUndefined();
    },
  );
});
