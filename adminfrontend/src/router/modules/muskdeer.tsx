import type { AppRouteObject } from '@/core/router/types';
import { createLazyRoute } from '@/core/router';
const routes: AppRouteObject[] = [{
 name: 'Muskdeer', path: 'muskdeer', meta: { title: 'routes:Muskdeer', icon: 'lucide:chart-candlestick', order: 20 },
 children: [
{ name: 'MuskdeerGateways', path: 'gateways', element: createLazyRoute(() => import('@/pages/app/muskdeer/gateways')), meta: { title: 'routes:MuskdeerGateways', authority: ['muskdeer:gateway:view', 'sys:platform_admin'] } },
{ name: 'MuskdeerTemplates', path: 'templates', element: createLazyRoute(() => import('@/pages/app/muskdeer/templates')), meta: { title: 'routes:MuskdeerTemplates', authority: ['muskdeer:template:view', 'sys:platform_admin'] } },
{ name: 'MuskdeerInstruments', path: 'instruments', element: createLazyRoute(() => import('@/pages/app/muskdeer/instruments')), meta: { title: 'routes:MuskdeerInstruments', authority: ['muskdeer:instrument:view', 'sys:platform_admin'] } },
 ]
}];
export default routes;
