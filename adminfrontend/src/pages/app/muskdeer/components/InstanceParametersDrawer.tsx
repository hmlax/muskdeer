import { Alert, Descriptions, Divider, Drawer } from 'antd';
import { useTranslation } from 'react-i18next';
import type { MuskdeerInstance } from '@/api/generated/admin/v1';

function ParameterSection<T extends object>({
  title,
  values,
  labels,
}: {
  title: string;
  values?: T;
  labels: Record<keyof T, string>;
}) {
  const { t } = useTranslation('muskdeer');
  return <>
    <Divider>{t(title)}</Divider>
    <Descriptions bordered column={1} size="small" items={Object.keys(labels).map((name) => {
      const key = name as keyof T;
      const value = values?.[key];
      return {
        key: name,
        label: t(labels[key]),
        children: <span style={{ overflowWrap: 'anywhere' }}>{value === null || value === undefined ? '—'
          : typeof value === 'boolean' ? t(value ? 'yes' : 'no') : String(value)}</span>,
      };
    })} />
  </>;
}

export default function InstanceParametersDrawer({ current, onClose }: {
  current: MuskdeerInstance;
  onClose: () => void;
}) {
  const { t } = useTranslation('muskdeer');
  const parameters = current.parameters;
  return <Drawer open title={`${t('viewParameters')} · ${current.symbol ?? current.instanceKey ?? ''}`}
    size={720} destroyOnHidden onClose={onClose}
    footer={null}>
    <Alert type="info" title={t('readOnlyParametersNotice')} />
    <Divider>{t('identity')}</Divider>
    <Descriptions bordered column={1} size="small" items={[
      { key: 'instanceKey', label: t('instanceKey'), children: current.instanceKey },
      { key: 'strategyType', label: t('strategyType'), children: t(current.strategyType ?? '') },
      { key: 'implementation', label: t('implementation'), children: current.implementation },
      { key: 'symbol', label: t('symbol'), children: current.symbol },
      { key: 'accountId', label: t('accountId'), children: current.accountId },
      { key: 'templateId', label: t('templateId'), children: current.templateId },
      { key: 'templateRevision', label: t('templateRevision'), children: current.templateRevision },
      { key: 'marketDataSource', label: t('marketDataSource'), children: current.marketDataSource },
      { key: 'revision', label: t('revision'), children: current.revision },
      { key: 'controlVersion', label: t('controlVersion'), children: current.controlVersion },
      { key: 'desiredRunning', label: t('desiredRunning'), children: t(current.desiredRunning ? 'yes' : 'no') },
      { key: 'runtime', label: t('runtime'), children: t(current.restartRequired ? 'restartRequired' : 'loaded') },
    ]} />
    <ParameterSection title="execution" values={parameters?.execution} labels={{
      pollIntervalMs: 'pollIntervalMs',
      operationTimeoutMs: 'operationTimeoutMs',
      leaseTtlMs: 'leaseTtlMs',
      leverage: 'leverage',
    }} />
    {parameters?.marketMaker && <ParameterSection title="market_maker" values={parameters.marketMaker} labels={{
      depth: 'depth',
      quantityFraction: 'quantityFraction',
      replacementFraction: 'replacementFraction',
      bridgeSlots: 'bridgeSlots',
    }} />}
    {parameters?.grid && <ParameterSection title="grid" values={parameters.grid} labels={{
      lowerPrice: 'lowerPrice',
      upperPrice: 'upperPrice',
      gridCount: 'gridCount',
      totalBudget: 'totalBudget',
      perOrderNotional: 'perOrderNotional',
      inventoryLimit: 'inventoryLimit',
      maxMarketAgeMs: 'maxMarketAgeMs',
    }} />}
    {parameters?.longGrid && <ParameterSection title="grid" values={parameters.longGrid} labels={{
      spacingFraction: 'spacingFraction',
      stopLossFraction: 'stopLossFraction',
      orderNotional: 'orderNotional',
      maximumPositionNotional: 'maximumPositionNotional',
      openOnStart: 'openOnStart',
      haltAfterStopLoss: 'haltAfterStopLoss',
    }} />}
    {parameters?.washTrade && <ParameterSection title="wash_trade" values={parameters.washTrade} labels={{
      developmentOnly: 'developmentOnly',
      mode: 'mode',
      runId: 'runId',
      intervalMs: 'intervalMs',
      maximumEvents: 'maximumEvents',
      quantity: 'quantity',
      maximumNotional: 'washMaximumNotional',
      quantityFraction: 'quantityFraction',
    }} />}
  </Drawer>;
}
