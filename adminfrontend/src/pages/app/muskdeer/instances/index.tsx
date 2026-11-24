import { useRef, useState } from 'react';
import { App, Button, Space, Switch, Tag, Tooltip } from 'antd';
import { useTranslation } from 'react-i18next';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import type { MuskdeerInstance } from '@/api/generated/admin/v1';
import { cancelMuskdeerOrders, getMuskdeerInstance, muskdeerReaders, startMuskdeerInstance, stopMuskdeerInstance, useMuskdeerWrite } from '@/api/hooks/muskdeer';
import { useAccess } from '@/core/access';
import { instanceActionMessageKey } from '@/api/hooks/muskdeer-errors';
import ResourceTable from '../components/ResourceTable';
import InstanceForm from '../components/InstanceForm';
import InstanceParametersDrawer from '../components/InstanceParametersDrawer';
import MakerCheckDrawer from '../components/MakerCheckDrawer';

export default function Instances() {
  const { t } = useTranslation('muskdeer');
  const { modal, message } = App.useApp();
  const { hasAccessByCodes } = useAccess();
  const actionRef = useRef<ActionType>(null);
  const [editor, setEditor] = useState<{ current?: MuskdeerInstance }>();
  const [checking, setChecking] = useState<MuskdeerInstance>();
  const [details, setDetails] = useState<MuskdeerInstance>();
  const [viewing, setViewing] = useState<number>();
  const [editingDetails, setEditingDetails] = useState(false);
  const [confirming, setConfirming] = useState<number>();
  const detailSequence = useRef(0);
  const start = useMuskdeerWrite(startMuskdeerInstance);
  const stop = useMuskdeerWrite(stopMuskdeerInstance);
  const cancel = useMuskdeerWrite(cancelMuskdeerOrders);
  const allowed = (action: string) => hasAccessByCodes([`muskdeer:instance:${action}`, "sys:tenant_manager"]);
  const busy = confirming !== undefined || start.isPending || stop.isPending || cancel.isPending;
  const act = async (row: MuskdeerInstance, kind: 'start' | 'stop' | 'cancel') => {
    if (busy) return;
    setConfirming(row.id);
    await modal.confirm({
      afterClose: () => setConfirming(undefined),
      title: t(kind === 'start' ? 'confirmStart' : kind === 'stop' ? 'confirmStop' : 'confirmCancel'),
      onOk: async () => {
        try {
          const request = { id: row.id, expectedRevision: row.revision, expectedControlVersion: row.controlVersion };
          if (kind === 'start') await start.mutateAsync(request);
          else if (kind === 'stop') await stop.mutateAsync(request);
          else await cancel.mutateAsync({ instanceId: row.id, expectedRevision: row.revision, cancelAll: true });
          message.success(t(kind === 'cancel' ? 'cancelPending' : 'saved'));
        } catch (error) {
          console.error('Instance action failed', error);
          message.error(t(instanceActionMessageKey(error)));
          throw error;
        } finally {
          await actionRef.current?.reload();
        }
      },
    });
  };
  const closeDetails = () => {
    detailSequence.current += 1;
    setDetails(undefined);
    setEditingDetails(false);
  };
  const openDetails = async (row: MuskdeerInstance) => {
    if (!row.id || viewing !== undefined) return;
    const sequence = ++detailSequence.current;
    setViewing(row.id);
    try {
      const current = await getMuskdeerInstance(row.id);
      if (sequence === detailSequence.current) setDetails(current);
    } catch (error) {
      console.error('Load instance details failed', error);
      message.error(t('failed'));
    } finally {
      setViewing(undefined);
    }
  };
  const editDetails = async () => {
    if (!details?.id || editingDetails || !allowed('update')) return;
    const sequence = detailSequence.current;
    setEditingDetails(true);
    try {
      const current = await getMuskdeerInstance(details.id);
      if (sequence !== detailSequence.current) return;
      setDetails(current);
      if (current.desiredRunning) {
        message.warning(t('stopBeforeEdit'));
        return;
      }
      closeDetails();
      setEditor({ current });
    } catch (error) {
      console.error('Load editable instance parameters failed', error);
      message.error(t('failed'));
    } finally {
      if (sequence === detailSequence.current) setEditingDetails(false);
    }
  };
  const columns: ProColumns<MuskdeerInstance>[] = [
    { title: t('search'), dataIndex: 'search', hideInTable: true },
    { title: t('strategyType'), dataIndex: 'strategyType', valueType: 'select', valueEnum: { market_maker: t('market_maker'), grid: t('grid'), wash_trade: t('wash_trade') }, width: 140 },
    {
      title: t('runningStatus'),
      dataIndex: 'desiredRunning',
      hideInTable: true,
      valueType: 'select',
      fieldProps: {
        options: [
          { label: t('allStatuses'), value: '' },
          { label: t('started'), value: 'true' },
          { label: t('stopped'), value: 'false' },
        ],
      },
      search: {
        transform: (value) => value === 'true' || value === 'false'
          ? { desiredRunning: value === 'true' }
          : {},
      },
    },
    { title: t('accountId'), dataIndex: 'accountId', width: 230 },
    { title: t('id'), dataIndex: 'id', hideInSearch: true, width: 80 },
    { title: t('instanceKey'), dataIndex: 'instanceKey', hideInSearch: true, width: 200 },
    { title: t('symbol'), dataIndex: 'symbol', hideInSearch: true, width: 120 },
    { title: t('marketDataSource'), dataIndex: 'marketDataSource', hideInSearch: true, width: 120 },
    { title: t('revision'), dataIndex: 'revision', hideInSearch: true, width: 90 },
    { title: t('controlVersion'), dataIndex: 'controlVersion', hideInSearch: true, width: 90 },
    { title: t('runtime'), hideInSearch: true, width: 150, render: (_, row) => <Tooltip title={row.runtimes?.map((v) => `${v.service}: ${v.available ? v.revision : '—'}`).join(' · ')}><Tag color={row.restartRequired ? 'orange' : 'green'}>{t(row.restartRequired ? 'restartRequired' : 'loaded')}</Tag></Tooltip> },
    { title: t('desiredRunning'), key: 'running', hideInSearch: true, fixed: 'right', width: 110, render: (_, row) => <Tooltip title={!row.desiredRunning && !row.accountEnabled ? t('enableAccountBeforeStart') : !row.desiredRunning && row.restartRequired ? t('restartRequired') : undefined}>
      <span><Switch checked={!!row.desiredRunning} checkedChildren={t('started')} unCheckedChildren={t('stopped')}
        aria-label={t(row.desiredRunning ? 'stopInstanceFor' : 'startInstanceFor', { instance: row.instanceKey })}
        loading={(start.isPending && start.variables?.id === row.id) || (stop.isPending && stop.variables?.id === row.id)}
        disabled={busy || (row.desiredRunning ? !allowed('stop') : !allowed('start') || !row.accountEnabled || row.restartRequired)}
        onChange={(running) => act(row, running ? 'start' : 'stop')} /></span>
    </Tooltip> },
    { title: t('actions'), valueType: 'option', width: 260, fixed: 'right', render: (_, row) => <Space wrap>
      {allowed('view') && <Button type="link" disabled={viewing !== undefined} loading={viewing === row.id} onClick={() => void openDetails(row)}>{t('instanceDetails')}</Button>}
      {allowed('view') && row.strategyType === 'market_maker' && <Button type="link" onClick={() => setChecking(row)}>{t('checkMaker')}</Button>}
      {allowed('cancel') && row.strategyType !== 'wash_trade' && <Button type="link" danger disabled={row.desiredRunning || busy} onClick={() => act(row, 'cancel')}>{t('cancelOrders')}</Button>}
    </Space> },
  ];
  return <>
    <ResourceTable<MuskdeerInstance> resource="instances" columns={columns} fetch={muskdeerReaders.instances} actionRef={actionRef} notice={t('parameterNotice')} search={{ defaultCollapsed: false }}
      toolbar={allowed('create') ? [<Button key="create" type="primary" onClick={() => setEditor({})}>{t('create')}</Button>] : []} />
    {editor && <InstanceForm current={editor.current} onClose={() => setEditor(undefined)} onSaved={() => void actionRef.current?.reload()} />}
    {checking && <MakerCheckDrawer current={checking} onClose={() => setChecking(undefined)} />}
    {details && <InstanceParametersDrawer current={details} onClose={closeDetails} onEdit={allowed('update') ? editDetails : undefined} editLoading={editingDetails} />}
  </>;
}
