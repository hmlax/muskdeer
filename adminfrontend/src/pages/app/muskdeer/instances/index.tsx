import { useRef, useState } from 'react';
import { App, Button, Space, Tag, Tooltip } from 'antd';
import { useTranslation } from 'react-i18next';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import type { MuskdeerInstance } from '@/api/generated/admin/v1';
import { cancelMuskdeerOrders, getMuskdeerInstance, muskdeerReaders, startMuskdeerInstance, stopMuskdeerInstance, useMuskdeerWrite } from '@/api/hooks/muskdeer';
import { useAccess } from '@/core/access';
import ResourceTable from '../components/ResourceTable';
import InstanceForm from '../components/InstanceForm';

export default function Instances() {
  const { t } = useTranslation('muskdeer');
  const { modal, message } = App.useApp();
  const { hasAccessByCodes } = useAccess();
  const actionRef = useRef<ActionType>(null);
  const [editor, setEditor] = useState<{ current?: MuskdeerInstance }>();
  const start = useMuskdeerWrite(startMuskdeerInstance);
  const stop = useMuskdeerWrite(stopMuskdeerInstance);
  const cancel = useMuskdeerWrite(cancelMuskdeerOrders);
  const allowed = (action: string) => hasAccessByCodes([`muskdeer:instance:${action}`, "sys:tenant_manager"]);
  const act = (row: MuskdeerInstance, kind: 'start' | 'stop' | 'cancel') => {
    modal.confirm({
      title: t(kind === 'start' ? 'confirmStart' : kind === 'stop' ? 'confirmStop' : 'confirmCancel'),
      onOk: async () => {
        try {
          const request = { id: row.id, expectedRevision: row.revision, expectedControlVersion: row.controlVersion };
          if (kind === 'start') await start.mutateAsync(request);
          else if (kind === 'stop') await stop.mutateAsync(request);
          else await cancel.mutateAsync({ instanceId: row.id, expectedRevision: row.revision, cancelAll: true });
          message.success(t(kind === 'cancel' ? 'cancelPending' : 'saved'));
        } catch (error) { console.error('Instance action failed', error); message.error(t('failed')); }
        finally { await actionRef.current?.reload(); }
      },
    });
  };
  const columns: ProColumns<MuskdeerInstance>[] = [
    { title: t('search'), dataIndex: 'search', hideInTable: true },
    { title: t('strategyType'), dataIndex: 'strategyType', valueType: 'select', valueEnum: { market_maker: t('market_maker'), grid: t('grid'), wash_trade: t('wash_trade') }, width: 140 },
    { title: t('accountId'), dataIndex: 'accountId', width: 230 },
    { title: t('id'), dataIndex: 'id', hideInSearch: true, width: 80 },
    { title: t('instanceKey'), dataIndex: 'instanceKey', hideInSearch: true, width: 200 },
    { title: t('symbol'), dataIndex: 'symbol', hideInSearch: true, width: 120 },
    { title: t('marketDataSource'), dataIndex: 'marketDataSource', hideInSearch: true, width: 120 },
    { title: t('revision'), dataIndex: 'revision', hideInSearch: true, width: 90 },
    { title: t('controlVersion'), dataIndex: 'controlVersion', hideInSearch: true, width: 90 },
    { title: t('desiredRunning'), hideInSearch: true, width: 110, render: (_, row) => <Tag color={row.desiredRunning ? 'green' : 'default'}>{t(row.desiredRunning ? 'yes' : 'no')}</Tag> },
    { title: t('runtime'), hideInSearch: true, width: 150, render: (_, row) => <Tooltip title={row.runtimes?.map((v) => `${v.service}: ${v.available ? v.revision : '—'}`).join(' · ')}><Tag color={row.restartRequired ? 'orange' : 'green'}>{t(row.restartRequired ? 'restartRequired' : 'loaded')}</Tag></Tooltip> },
    { title: t('actions'), valueType: 'option', width: 300, fixed: 'right', render: (_, row) => <Space wrap>
      {allowed('update') && <Button type="link" disabled={row.desiredRunning} onClick={async () => {
        try { if (row.id) setEditor({ current: await getMuskdeerInstance(row.id) }); }
        catch (error) { console.error('Load current instance failed', error); message.error(t('failed')); }
      }}>{t('edit')}</Button>}
      {allowed('start') && <Button type="link" disabled={row.desiredRunning || row.restartRequired || start.isPending} onClick={() => act(row, 'start')}>{t('start')}</Button>}
      {allowed('stop') && <Button type="link" disabled={!row.desiredRunning || stop.isPending} onClick={() => act(row, 'stop')}>{t('stop')}</Button>}
      {allowed('cancel') && row.strategyType !== 'wash_trade' && <Button type="link" danger disabled={row.desiredRunning || cancel.isPending} onClick={() => act(row, 'cancel')}>{t('cancelOrders')}</Button>}
    </Space> },
  ];
  return <>
    <ResourceTable<MuskdeerInstance> resource="instances" columns={columns} fetch={muskdeerReaders.instances} actionRef={actionRef} notice={t('parameterNotice')}
      toolbar={allowed('create') ? [<Button key="create" type="primary" onClick={() => setEditor({})}>{t('create')}</Button>] : []} />
    {editor && <InstanceForm current={editor.current} onClose={() => setEditor(undefined)} onSaved={() => void actionRef.current?.reload()} />}
  </>;
}
