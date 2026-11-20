import type { ComponentType } from 'react';

// This build contains tenant features. Global administration stays in platform-admin.
const sharedPages = import.meta.glob<{ default: ComponentType }>([
  '../src/pages/app/**/*.tsx',
  '!../src/pages/app/dashboard/analytics/**',
  '!../src/pages/app/tenant/**',
  '!../src/pages/app/muskdeer/{account-bindings,templates,instruments,gateways}/**',
  '!../src/pages/app/permission/{api,menu,permission}/**',
  '!../src/pages/app/system/{config,dict,language}/**',
]);

export const pageModules = {
  ...sharedPages,
  '../src/pages/app/dashboard/analytics/index.tsx': () => import('./pages/dashboard/analytics'),
};
