import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ApiGrant } from '@/api/generated/admin/v1';

const fixture = vi.hoisted(() => ({
  authorizationLoaded: true,
  apiPermissions: [] as ApiGrant[],
  types: vi.fn(),
  entries: vi.fn(),
}));

vi.mock('@/stores/user', () => ({ useUserStore: { getState: () => fixture } }));
vi.mock('@/api/hooks/dict', () => ({
  fetchListDictTypes: fixture.types,
  fetchListDictEntries: fixture.entries,
}));
vi.mock('@/core', () => ({ PaginationQuery: class {} }));

import { fetchAllDictEntries, getDictEntriesByTypeCode, resetDictCache } from './useDictCache';

beforeEach(() => {
  resetDictCache();
  fixture.authorizationLoaded = true;
  fixture.apiPermissions = [];
  fixture.types.mockReset().mockResolvedValue({ items: [{ id: 1, typeCode: 'status' }] });
  fixture.entries.mockReset().mockResolvedValue({ items: [{ typeId: 1, entryValue: 'ON' }] });
});

describe('dictionary preload authorization', () => {
  it.each([
    [],
    [{ method: 'GET', path: '/admin/v1/dict/types' }],
    [{ method: 'GET', path: '/admin/v1/dict/entries' }],
    [{ method: 'POST', path: '*' }],
  ])('does not request either dictionary without both read grants: %j', async (...grants) => {
    fixture.apiPermissions = grants as ApiGrant[];
    await fetchAllDictEntries();
    expect(fixture.types).not.toHaveBeenCalled();
    expect(fixture.entries).not.toHaveBeenCalled();
  });

  it('waits for current authorization even when old grants exist', async () => {
    fixture.authorizationLoaded = false;
    fixture.apiPermissions = [{ method: '*', path: '*' }];
    await fetchAllDictEntries();
    expect(fixture.types).not.toHaveBeenCalled();
  });

  it.each([
    [{ method: '*', path: '*' }],
    [
      { method: 'GET', path: '/admin/v1/dict/types' },
      { method: 'GET', path: '/admin/v1/dict/entries' },
    ],
  ])('loads and reuses dictionaries for an authorized identity: %j', async (...grants) => {
    fixture.apiPermissions = grants as ApiGrant[];
    await fetchAllDictEntries();
    await fetchAllDictEntries();
    expect(fixture.types).toHaveBeenCalledTimes(1);
    expect(fixture.entries).toHaveBeenCalledTimes(1);
    expect(getDictEntriesByTypeCode('status')).toEqual([{ typeId: 1, entryValue: 'ON' }]);
  });

  it('clears prior dictionaries when the next identity lacks dictionary access', async () => {
    fixture.apiPermissions = [{ method: '*', path: '*' }];
    await fetchAllDictEntries();
    fixture.apiPermissions = [];
    await fetchAllDictEntries();
    expect(getDictEntriesByTypeCode('status')).toEqual([]);
    expect(fixture.types).toHaveBeenCalledTimes(1);
  });
});
