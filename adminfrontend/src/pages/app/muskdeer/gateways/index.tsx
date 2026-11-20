import { useTranslation } from 'react-i18next';
import { Tag } from 'antd';
import type { ProColumns } from '@ant-design/pro-components';
import type { MuskdeerGateway } from '@/api/generated/admin/v1';
import { muskdeerReaders } from '@/api/hooks/muskdeer';
import ResourceTable from '../components/ResourceTable';

export default function Gateways() {
  const { t } = useTranslation('muskdeer');
  const columns: ProColumns<MuskdeerGateway>[] = [
    { title: t('gatewayKey'), dataIndex: 'key', hideInSearch: true },
    { title: t('executionVenue'), dataIndex: 'executionVenue', hideInSearch: true },
    { title: t('protocol'), dataIndex: 'protocol', hideInSearch: true },
    { title: t('target'), dataIndex: 'target', hideInSearch: true },
    { title: t('configured'), hideInSearch: true, render: (_, row) => <Tag>{t(row.configured ? 'yes' : 'no')}</Tag> },
  ];
  return <ResourceTable<MuskdeerGateway> resource="gateways" columns={columns} fetch={muskdeerReaders.gateways} notice={t('gatewayNotice')} rowKey="key" />;
}
