import { formatTimestampMs, timestampMillis } from '../components/timestamp';
import { useState } from 'react';
import { Alert, Button, Card, Descriptions, Input, Space, Table } from 'antd';
import { useTranslation } from 'react-i18next';
import { useSearchParams } from 'react-router-dom';
import { useMuskdeerSnapshot } from '@/api/hooks/muskdeer';
import ContentContainer from '@/layouts/components/PageContainer/ContentContainer';

export default function Snapshot() {
  const { t } = useTranslation('muskdeer');
  const [params] = useSearchParams();
  const [accountId, setAccountId] = useState(params.get('accountId') ?? '');
  const query = useMuskdeerSnapshot(accountId);
  const snapshot = query.data;
  const expiry = timestampMillis(snapshot?.validUntilMs);
  const expired = !!snapshot && (expiry === undefined || expiry <= Date.now());
  return <ContentContainer padding="16px">
    <Alert type="info" title={t('snapshotNotice')} />
    <Space style={{ margin: '16px 0' }}>
      <Input.Search defaultValue={accountId} placeholder={t('accountId')} enterButton={t('loadSnapshot')} onSearch={(value) => setAccountId(value.trim())} style={{ width: 460 }} />
      <Button disabled={!accountId} loading={query.isFetching} onClick={() => void query.refetch()}>{t('refresh')}</Button>
    </Space>
    {query.error && <Alert type="error" title={query.error.message} />}
    {expired && <Alert type="warning" title={t('staleSnapshot')} />}
    {snapshot && <>
      <Card style={{ marginBottom: 16 }}>
        <Descriptions items={[
          { key: 'account', label: t('accountId'), children: snapshot.accountId },
          { key: 'symbol', label: t('symbol'), children: snapshot.symbol },
          { key: 'available', label: t('available'), children: snapshot.available },
          { key: 'mark', label: t('markPrice'), children: snapshot.markPrice },
          { key: 'observed', label: t('observedAtMs'), children: formatTimestampMs(snapshot.observedAtMs) },
          { key: 'valid', label: t('validUntilMs'), children: formatTimestampMs(snapshot.validUntilMs) },
        ]} />
      </Card>
      <Card title={t('activeOrders')} style={{ marginBottom: 16 }}>
        <Table rowKey="orderId" dataSource={snapshot.orders ?? []} pagination={{ pageSize: 20 }} scroll={{ x: 1200 }} columns={['orderId', 'clientId', 'side', 'price', 'quantity', 'executedQuantity', 'status'].map((field) => ({ title: t(field), dataIndex: field }))} />
      </Card>
      <Card title={t('positions')}>
        <Table rowKey="symbol" dataSource={snapshot.positions ?? []} pagination={false} columns={['symbol', 'signedQuantity', 'entryPrice', 'leverage'].map((field) => ({ title: t(field), dataIndex: field }))} />
      </Card>
    </>}
  </ContentContainer>;
}
