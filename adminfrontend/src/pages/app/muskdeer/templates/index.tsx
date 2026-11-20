import { useTranslation } from 'react-i18next';
import type { ProColumns } from '@ant-design/pro-components';
import { Tag } from 'antd';
import type { MuskdeerTemplate } from '@/api/generated/admin/v1';
import { muskdeerReaders } from '@/api/hooks/muskdeer';
import ResourceTable from '../components/ResourceTable';
export default function Page() {
 const { t } = useTranslation('muskdeer');
 const columns: ProColumns<MuskdeerTemplate>[] = [
{ title: t('search'), dataIndex: 'search', hideInTable: true },
{ title: t('name'), dataIndex: 'name', hideInSearch: true, width: 160 },
{ title: t('strategyType'), dataIndex: 'strategyType', hideInSearch: true, width: 160 },
{ title: t('implementation'), dataIndex: 'implementation', hideInSearch: true, width: 160 },
{ title: t('revision'), dataIndex: 'revision', hideInSearch: true, width: 160 },
{ title: t('enabled'), dataIndex: 'enabled', hideInSearch: true, width: 160, render: (_, row) => <Tag color={row.enabled ? 'green' : 'default'}>{t(row.enabled ? 'yes' : 'no')}</Tag> },
{ title: t('requiresOrderBook'), dataIndex: 'requiresOrderBook', hideInSearch: true, width: 160, render: (_, row) => <Tag color={row.requiresOrderBook ? 'green' : 'default'}>{t(row.requiresOrderBook ? 'yes' : 'no')}</Tag> },
{ title: t('requiresAccount'), dataIndex: 'requiresAccount', hideInSearch: true, width: 160, render: (_, row) => <Tag color={row.requiresAccount ? 'green' : 'default'}>{t(row.requiresAccount ? 'yes' : 'no')}</Tag> },
{ title: t('tradesMode'), dataIndex: 'tradesMode', hideInSearch: true, width: 160 },
{ title: t('executionFields'), dataIndex: 'executionFields', hideInSearch: true, width: 160, render: (_, row) => row.executionFields?.join(', ') ?? '—' },
{ title: t('parameterFields'), dataIndex: 'parameterFields', hideInSearch: true, width: 160, render: (_, row) => row.parameterFields?.join(', ') ?? '—' },
];
 return <ResourceTable<MuskdeerTemplate> resource="templates" columns={columns} fetch={muskdeerReaders.templates} />;
}
