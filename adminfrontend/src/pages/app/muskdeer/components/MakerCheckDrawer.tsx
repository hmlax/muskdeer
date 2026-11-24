import { Alert, Button, Descriptions, Drawer, Space, Spin } from 'antd';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { MuskdeerInstance } from '@/api/generated/admin/v1';
import { checkMuskdeerInstance, useMuskdeerScope } from '@/api/hooks/muskdeer';
import { makerCheckMessageKey } from '@/api/hooks/muskdeer-errors';
import { formatNotional } from './notional';
import { formatTimestampMs } from './timestamp';

export default function MakerCheckDrawer({ current, onClose }: {
  current: MuskdeerInstance;
  onClose: () => void;
}) {
  const { t } = useTranslation('muskdeer');
  const scope = useMuskdeerScope();
  const check = useQuery({ queryKey: ['muskdeer', scope, 'makerCheck', current.id],
    queryFn: () => checkMuskdeerInstance(current.id ?? 0), enabled: !!current.id, retry: 0, staleTime: 0 });
  const result = check.data;
  const observed = formatTimestampMs(result?.snapshotObservedAtMs) !== '—';
  return <Drawer open title={`${t('checkMaker')} · ${current.symbol ?? ''}`} size={640} destroyOnHidden onClose={onClose}>
    <Space orientation="vertical" style={{ width: '100%' }} size="middle">
      <Alert type="info" title={t('makerCheckNotice')} />
      <Button loading={check.isFetching} onClick={() => void check.refetch()}>{t('refreshCheck')}</Button>
      {check.isPending && <Spin />}
      {check.isError && <Alert type="error" title={t('failed')} />}
      {result && <>
        <Alert type={result.canStart ? 'success' : 'warning'} title={t(makerCheckMessageKey(result.reason))} />
        <Descriptions bordered column={1} size="small" items={[
          { key: 'instance', label: t('instanceKey'), children: current.instanceKey },
          { key: 'target', label: t('targetNotional'), children: formatNotional(result.targetNotional) },
          { key: 'maximum', label: t('maximumNotional'), children: formatNotional(result.maximumNotional) },
          { key: 'single', label: t('maximumOrderNotional'), children: formatNotional(result.maximumOrderNotional) },
          { key: 'count', label: t('targetOrderCount'), children: result.targetNotional ? result.targetOrderCount ?? 0 : '—' },
          { key: 'buy', label: t('activeBuyOrders'), children: observed ? result.activeBuyOrders ?? 0 : '—' },
          { key: 'sell', label: t('activeSellOrders'), children: observed ? result.activeSellOrders ?? 0 : '—' },
          { key: 'available', label: t('availableQuote'), children: observed ? result.availableQuote : '—' },
          { key: 'observed', label: t('snapshotObservedAt'), children: formatTimestampMs(result.snapshotObservedAtMs) },
          { key: 'checked', label: t('checkedAt'), children: formatTimestampMs(result.checkedAtMs) },
          { key: 'version', label: t('revision'), children: result.revision },
        ]} />
      </>}
    </Space>
  </Drawer>;
}
