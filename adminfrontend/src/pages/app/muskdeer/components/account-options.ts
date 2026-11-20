import type { ListQuery } from './ResourceTable';

// Keep all identifier digits; registration never converts these values to Number.
export function validTradingID(value: unknown): boolean {
  return typeof value === 'string' && /^[1-9]\d{0,18}$/.test(value) && BigInt(value) <= 9223372036854775807n;
}

export async function loadAccountOptions<T>(reader: (query: ListQuery) => Promise<{ items?: T[]; total?: number }>): Promise<T[]> {
  const result: T[] = [];
  for (let page = 1; ; page++) {
    const response = await reader({ page, pageSize: 200 });
    const items = response.items ?? [];
    result.push(...items);
    if (result.length >= Number(response.total ?? 0)) return result;
    if (items.length === 0) throw new Error('Account selector pagination returned an incomplete page');
  }
}
