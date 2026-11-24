import { expect, it } from 'vitest';
import { createAccessKeyServiceClient, createMuskdeerServiceClient, type ClientTransport } from '@/api/generated/admin/v1';

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

it.each([undefined, false, true])('keeps account user ID digits and optional enabled=%s in query', async (enabled) => {
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
  await createMuskdeerServiceClient(transport).ListAccounts({
    userId: '9007199254740993', enabled, page: 2, pageSize: 20,
  });
  const query = new URL(requested, 'http://localhost').searchParams;
  expect(query.get('userId')).toBe('9007199254740993');
  expect(query.get('enabled')).toBe(enabled === undefined ? null : String(enabled));
  expect(query.get('page')).toBe('2');
});
