import { useTranslation } from 'react-i18next';
import type { ProColumns } from '@ant-design/pro-components';
import type { MuskdeerInstrument } from '@/api/generated/admin/v1';
import { muskdeerReaders } from '@/api/hooks/muskdeer';
import ResourceTable from '../components/ResourceTable';
export default function Page() {
 const { t } = useTranslation('muskdeer');
 const columns: ProColumns<MuskdeerInstrument>[] = [
{ title: t('search'), dataIndex: 'search', hideInTable: true },
{ title: t('id'), dataIndex: 'id', hideInSearch: true, width: 160 },
{ title: t('symbol'), dataIndex: 'symbol', hideInSearch: true, width: 160 },
{ title: t('baseAsset'), dataIndex: 'baseAsset', hideInSearch: true, width: 160 },
{ title: t('quoteAsset'), dataIndex: 'quoteAsset', hideInSearch: true, width: 160 },
{ title: t('status'), dataIndex: 'status', hideInSearch: true, width: 160 },
{ title: t('executionVenue'), dataIndex: 'executionVenue', hideInSearch: true, width: 160 },
{ title: t('priceTick'), dataIndex: 'priceTick', hideInSearch: true, width: 160 },
{ title: t('quantityStep'), dataIndex: 'quantityStep', hideInSearch: true, width: 160 },
{ title: t('minimumNotional'), dataIndex: 'minimumNotional', hideInSearch: true, width: 160 },
];
 return <ResourceTable<MuskdeerInstrument> resource="instruments" columns={columns} fetch={muskdeerReaders.instruments} />;
}
