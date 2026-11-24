// Display rounding only. Requests and editable fields keep their decimal strings.
export function formatNotional(value?: string): string {
  const match = value?.match(/^(-?)(\d+)(?:\.(\d+))?$/);
  if (!match) return '—';
  const fraction = match[3] ?? '';
  let cents = BigInt(match[2]) * 100n + BigInt((fraction + '00').slice(0, 2));
  if (fraction.length > 2 && fraction[2] >= '5') cents += 1n;
  const sign = match[1] === '-' && cents !== 0n ? '-' : '';
  return `${sign}${cents / 100n}.${String(cents % 100n).padStart(2, '0')}`;
}

// Remove database padding while retaining every significant editable decimal.
export function editableNotional(value?: string): string | undefined {
  const match = value?.match(/^(\d+)(?:\.(\d+))?$/);
  if (!match) return value;
  return `${match[1]}.${(match[2] ?? '').replace(/0+$/, '').padEnd(2, '0')}`;
}
