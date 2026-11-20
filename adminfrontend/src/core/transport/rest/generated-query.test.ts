import { expect, it } from 'vitest';
import { createAccessKeyServiceClient, type ClientTransport } from '@/api/generated/admin/v1';

it('sends repeated sorting messages and nested filters in Hertz JSON query values', async () => {
  let requested = '';
  const transport: ClientTransport = {
    async unary(path) {
      requested = path;
      return { items: [], total: 0 };
    },
    serverStream() {
      throw new Error('not used');
    },
    duplexStream() {
      throw new Error('not used');
    },
  };
  const filterExpr = {
    type: 'AND' as const,
    conditions: [{ field: 'tenant_id', op: 'EQ' as const, value: '7' }],
  };
  const sorting = [
    { field: 'id', direction: 'DESC' as const },
    { field: 'name', direction: 'ASC' as const },
  ];
  await createAccessKeyServiceClient(transport).List({
    filterExpr,
    sorting,
    offset: 0,
    noPaging: false,
  });
  const query = new URL(requested, 'http://localhost').searchParams;
  expect(JSON.parse(query.get('filterExpr')!)).toEqual(filterExpr);
  expect(query.getAll('sorting').map((value) => JSON.parse(value))).toEqual(sorting);
  expect(query.get('offset')).toBe('0');
  expect(query.get('noPaging')).toBe('false');
});
