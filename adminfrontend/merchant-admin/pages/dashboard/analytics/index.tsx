import { Alert, Button, Card, Col, Row, Statistic } from 'antd';
import { useTranslation } from 'react-i18next';
import { useMuskdeerOverview } from '@/api/hooks/muskdeer';
import ContentContainer from '@/layouts/components/PageContainer/ContentContainer';

export default function Analytics() {
  const { t } = useTranslation('muskdeer');
  const query = useMuskdeerOverview();
  const values = [
    ['templatesCount', query.data?.templates],
    ['instancesCount', query.data?.instances],
    ['runningCount', query.data?.desiredRunning],
    ['accountsCount', query.data?.accounts],
    ['ordersCount', query.data?.orderRecords],
    ['publishedCount', query.data?.syntheticPublished],
    ['unknownCount', query.data?.syntheticUnknown],
  ] as const;
  return (
    <ContentContainer padding="16px">
      {query.error && <Alert type="error" title={query.error.message} />}
      <Row gutter={[16, 16]}>
        {values.map(([label, value]) => (
          <Col xs={24} sm={12} lg={8} key={label}>
            <Card loading={query.isLoading}>
              <Statistic title={t(label)} value={value ?? '—'} />
            </Card>
          </Col>
        ))}
      </Row>
      <Alert type="info" title={t('syntheticNotice')} style={{ marginTop: 16 }} />
      <Button
        onClick={() => void query.refetch()}
        loading={query.isFetching}
        style={{ marginTop: 16 }}
      >
        {t('refresh')}
      </Button>
    </ContentContainer>
  );
}
