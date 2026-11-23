import { formatTimestampMs } from '../components/timestamp';
import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { App, Button, Tag } from 'antd';
import { useAccess } from '@/core/access';
import type { MuskdeerAccount } from '@/api/generated/admin/v1';
import { enableMuskdeerAccount, muskdeerReaders, useMuskdeerWrite } from '@/api/hooks/muskdeer';
import ResourceTable from '../components/ResourceTable';
import AccountForm from '../components/AccountForm';
export default function Page() {
 const { t } = useTranslation('muskdeer');
 const { hasAccessByCodes } = useAccess();
 const { modal, message } = App.useApp();
 const enable = useMuskdeerWrite(enableMuskdeerAccount);
 const actionRef = useRef<ActionType>(null);
 const [adding, setAdding] = useState(false);
 const columns: ProColumns<MuskdeerAccount>[] = [
{ title: t('search'), dataIndex: 'search', hideInTable: true },
{ title: t('accountId'), dataIndex: 'accountId', hideInSearch: true, width: 160 },
{ title: t('symbol'), dataIndex: 'symbol', hideInSearch: true, width: 160 },
{ title: t('userId'), dataIndex: 'userId', hideInSearch: true, width: 160 },
{ title: t('fundId'), dataIndex: 'fundId', hideInSearch: true, width: 160 },
{ title: t('enabled'), dataIndex: 'enabled', hideInSearch: true, width: 160, render: (_, row) => <Tag color={row.enabled ? 'green' : 'default'}>{t(row.enabled ? 'yes' : 'no')}</Tag> },
{ title: t('maximumNotional'), dataIndex: 'maximumNotional', hideInSearch: true, width: 160 },
{ title: t('maximumOrderNotional'), dataIndex: 'maximumOrderNotional', hideInSearch: true, width: 160 },
{ title: t('leverage'), dataIndex: 'leverage', hideInSearch: true, width: 160 },
{ title: t('observedNotional'), dataIndex: 'observedNotional', hideInSearch: true, width: 160 },
{ title: t('reservedNotional'), dataIndex: 'reservedNotional', hideInSearch: true, width: 160 },
{ title: t('availableQuote'), dataIndex: 'availableQuote', hideInSearch: true, width: 160 },
{ title: t('snapshotExpiresAtMs'), dataIndex: 'snapshotExpiresAtMs', hideInSearch: true, width: 160, render: (_, row) => formatTimestampMs(row.snapshotExpiresAtMs) },
{ title: t('actions'), valueType: 'option', fixed: 'right', width: 140, render: (_, row) => hasAccessByCodes(['muskdeer:account:enable', 'sys:tenant_manager']) && <Button type="link" disabled={row.enabled || enable.isPending} onClick={() => modal.confirm({
 title: t('enableAccount'),
 content: t('confirmEnableAccount', { symbol: row.symbol, maximumNotional: row.maximumNotional, maximumOrderNotional: row.maximumOrderNotional }),
 onOk: async () => {
  try { await enable.mutateAsync({ id: row.id, expectedUpdatedAtMs: row.updatedAtMs }); message.success(t('accountEnabledSaved')); }
  catch (error) { console.error('Enable trading account failed', error); message.error(t('failed')); throw error; }
  finally { await actionRef.current?.reload(); }
 },
})}>{t('enableAccount')}</Button> },
];
 return <>
  <ResourceTable<MuskdeerAccount> resource="accounts" columns={columns} fetch={muskdeerReaders.accounts} actionRef={actionRef}
   toolbar={hasAccessByCodes(['muskdeer:account:create']) ? [<Button key="create" type="primary" onClick={() => setAdding(true)}>{t('addAccount')}</Button>] : []} />
  {adding && <AccountForm onClose={() => setAdding(false)} onSaved={() => void actionRef.current?.reload()} />}
 </>;
}
