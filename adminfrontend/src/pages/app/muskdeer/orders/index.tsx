import { formatTimestampMs } from '../components/timestamp';
import { useTranslation } from 'react-i18next';
import type { ProColumns } from '@ant-design/pro-components';
import type { MuskdeerOrderRecord } from '@/api/generated/admin/v1';
import { muskdeerReaders } from '@/api/hooks/muskdeer';
import ResourceTable from '../components/ResourceTable';
export default function Page() {
 const { t } = useTranslation('muskdeer');
 const columns: ProColumns<MuskdeerOrderRecord>[] = [
{ title: t('symbol'), dataIndex: 'symbol', hideInTable: true },
{ title: t('state'), dataIndex: 'state', hideInTable: true },
{ title: t('accountId'), dataIndex: 'accountId', hideInTable: true },
{ title: t('id'), dataIndex: 'id', hideInSearch: true, width: 160 },
{ title: t('createdAtMs'), dataIndex: 'createdAtMs', hideInSearch: true, width: 160, render: (_, row) => formatTimestampMs(row.createdAtMs) },
{ title: t('accountId'), dataIndex: 'accountId', hideInSearch: true, width: 160 },
{ title: t('symbol'), dataIndex: 'symbol', hideInSearch: true, width: 160 },
{ title: t('instanceId'), dataIndex: 'instanceId', hideInSearch: true, width: 160 },
{ title: t('revision'), dataIndex: 'revision', hideInSearch: true, width: 160 },
{ title: t('side'), dataIndex: 'side', hideInSearch: true, width: 160 },
{ title: t('price'), dataIndex: 'price', hideInSearch: true, width: 160 },
{ title: t('quantity'), dataIndex: 'quantity', hideInSearch: true, width: 160 },
{ title: t('state'), dataIndex: 'state', hideInSearch: true, width: 160 },
{ title: t('orderId'), dataIndex: 'orderId', hideInSearch: true, width: 160 },
{ title: t('clientId'), dataIndex: 'clientId', hideInSearch: true, width: 160 },
{ title: t('reservedNotional'), dataIndex: 'reservedNotional', hideInSearch: true, width: 160 },
];
 return <ResourceTable<MuskdeerOrderRecord> resource="orders" columns={columns} fetch={muskdeerReaders.orders} />;
}
