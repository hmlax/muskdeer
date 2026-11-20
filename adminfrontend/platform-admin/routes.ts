import type { AppRouteObject } from '@/core/router/types';

const modules = import.meta.glob<{ default: AppRouteObject[] }>('../src/router/modules/**/*.tsx', {
  eager: true,
});

export const businessRoutes = Object.values(modules).flatMap((module) => module.default);
