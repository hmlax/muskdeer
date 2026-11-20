import { useCallback, useMemo, useSyncExternalStore } from 'react';

/** Subscribe to browser media state without mirroring it into React effects. */
export function useMediaQuery(query: string): boolean {
  const media = useMemo(
    () =>
      typeof window !== 'undefined' && window.matchMedia ? window.matchMedia(query) : undefined,
    [query],
  );
  const subscribe = useCallback(
    (onChange: () => void) => {
      media?.addEventListener('change', onChange);
      return () => media?.removeEventListener('change', onChange);
    },
    [media],
  );
  return useSyncExternalStore(
    subscribe,
    () => media?.matches ?? false,
    () => false,
  );
}
