import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { App, Button, Switch } from 'antd';
import { useAccess } from '@/core/access';
import type { MuskdeerAccount } from '@/api/generated/admin/v1';
import { enableMuskdeerAccount, disableMuskdeerAccount, muskdeerReaders, useMuskdeerWrite } from '@/api/hooks/muskdeer';
import ResourceTable from '../components/ResourceTable';
import AccountForm from '../components/AccountForm';
import AccountParametersForm from '../components/AccountParametersForm';
import { formatTimestampMs } from '../components/timestamp';
import { formatNotional } from '../components/notional';
import { validTradingID } from '../components/account-options';

export default function Page() {
  const { t } = useTranslation('muskdeer');
  const { hasAccessByCodes } = useAccess();
  const { modal, message } = App.useApp();
  const toggle = useMuskdeerWrite(({ enabled, id, expectedUpdatedAtMs }: { enabled: boolean; id?: number; expectedUpdatedAtMs?: number }) =>
    enabled ? enableMuskdeerAccount({ id, expectedUpdatedAtMs }) : disableMuskdeerAccount({ id, expectedUpdatedAtMs }));
  const actionRef = useRef<ActionType>(null);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<MuskdeerAccount>();
  const [confirming, setConfirming] = useState<number>();
  const columns: ProColumns<MuskdeerAccount>[] = [
    { title: t('search'), dataIndex: 'search', hideInTable: true },
    { title: t('accountId'), dataIndex: 'accountId', hideInSearch: true, width: 160 },
    { title: t('symbol'), dataIndex: 'symbol', hideInSearch: true, width: 160 },
    {
      title: t('userId'), dataIndex: 'userId', width: 160,
      formItemProps: { rules: [{ validator: (_rule, value) => value === undefined || value === '' || validTradingID(value)
        ? Promise.resolve() : Promise.reject(new Error(t('tradingIDRequired'))) }] },
    },
    {
      title: t('accountStatus'), dataIndex: 'enabledFilter', hideInTable: true, valueType: 'select',
      fieldProps: { options: [
        { label: t('allStatuses'), value: '' },
        { label: t('accountEnabledStatus'), value: 'true' },
        { label: t('accountDisabledStatus'), value: 'false' },
      ] },
      search: { transform: (value) => value === 'true' || value === 'false' ? { enabled: value === 'true' } : {} },
    },
    { title: t('fundId'), dataIndex: 'fundId', hideInSearch: true, width: 160 },
    { title: t('enabled'), dataIndex: 'enabled', hideInSearch: true, width: 100, render: (_, row) => <Switch checked={!!row.enabled}
      aria-label={t(row.enabled ? 'disableAccountFor' : 'enableAccountFor', { symbol: row.symbol })}
      checkedChildren={t('yes')} unCheckedChildren={t('no')}
      loading={toggle.isPending && toggle.variables?.id === row.id}
      disabled={confirming !== undefined || toggle.isPending || !hasAccessByCodes([row.enabled ? 'muskdeer:account:disable' : 'muskdeer:account:enable', 'sys:tenant_manager'])}
      onChange={async (enabled) => {
        setConfirming(row.id);
        await modal.confirm({
          title: t(enabled ? 'enableAccount' : 'disableAccount'),
          content: t(enabled ? 'confirmEnableAccount' : 'confirmDisableAccount', { symbol: row.symbol }),
          afterClose: () => setConfirming(undefined),
          onOk: async () => {
            try {
              await toggle.mutateAsync({ id: row.id, expectedUpdatedAtMs: row.updatedAtMs, enabled });
              message.success(t(enabled ? 'accountEnabledSaved' : 'accountDisabledSaved'));
            } catch (error) {
              console.error('Set trading account enablement failed', error);
              message.error(t('failed'));
              throw error;
            } finally {
              await actionRef.current?.reload();
            }
          },
        });
      }} /> },
    { title: t('maximumNotional'), dataIndex: 'maximumNotional', hideInSearch: true, width: 160, render: (_, row) => formatNotional(row.maximumNotional) },
    { title: t('maximumOrderNotional'), dataIndex: 'maximumOrderNotional', hideInSearch: true, width: 160, render: (_, row) => formatNotional(row.maximumOrderNotional) },
    { title: t('leverage'), dataIndex: 'leverage', hideInSearch: true, width: 100 },
    { title: t('observedNotional'), dataIndex: 'observedNotional', hideInSearch: true, width: 160, render: (_, row) => formatNotional(row.observedNotional) },
    { title: t('reservedNotional'), dataIndex: 'reservedNotional', hideInSearch: true, width: 160, render: (_, row) => formatNotional(row.reservedNotional) },
    { title: t('availableQuote'), dataIndex: 'availableQuote', hideInSearch: true, width: 160, render: (_, row) => formatNotional(row.availableQuote) },
    { title: t('snapshotExpiresAtMs'), dataIndex: 'snapshotExpiresAtMs', hideInSearch: true, width: 160, render: (_, row) => formatTimestampMs(row.snapshotExpiresAtMs) },
    { title: t('actions'), valueType: 'option', fixed: 'right', width: 100, render: (_, row) => hasAccessByCodes(['muskdeer:account:update', 'sys:tenant_manager'])
      ? <Button type="link" onClick={() => setEditing(row)}>{t('accountDetails')}</Button> : null },
  ];
  return <>
    <ResourceTable<MuskdeerAccount> resource="accounts" columns={columns} search={{ defaultCollapsed: false }} fetch={muskdeerReaders.accounts} actionRef={actionRef}
      toolbar={hasAccessByCodes(['muskdeer:account:create']) ? [<Button key="create" type="primary" onClick={() => setAdding(true)}>{t('addAccount')}</Button>] : []} />
    {editing && <AccountParametersForm current={editing} onClose={() => setEditing(undefined)} onSaved={() => void actionRef.current?.reload()} />}
    {adding && <AccountForm onClose={() => setAdding(false)} onSaved={() => void actionRef.current?.reload()} />}
  </>;
}
