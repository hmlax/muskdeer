// Protobuf JSON encodes int64 milliseconds as strings, including the zero sentinel.
export function timestampMillis(value: unknown): number | undefined {
  if (typeof value !== 'number' && (typeof value !== 'string' || !/^\d+$/.test(value))) {
    return undefined;
  }
  const milliseconds = Number(value);
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0 || milliseconds > 8640000000000000) {
    return undefined;
  }
  return milliseconds;
}

export function formatTimestampMs(value: unknown): string {
  const milliseconds = timestampMillis(value);
  return milliseconds === undefined ? '—' : new Date(milliseconds).toLocaleString();
}
