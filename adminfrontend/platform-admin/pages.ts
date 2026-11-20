import type { ComponentType } from 'react';

export const pageModules = import.meta.glob<{ default: ComponentType }>([
  '../src/pages/app/**/*.tsx',
  '!../src/pages/app/muskdeer/{instances,accounts,orders,cancellations,synthetic,snapshot,account-bindings}/**',
]);
