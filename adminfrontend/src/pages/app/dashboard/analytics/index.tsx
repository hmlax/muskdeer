import { Card, Space, Typography } from 'antd';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '@/stores';
import ContentContainer from '@/layouts/components/PageContainer/ContentContainer';

export default function Home() {
  const { t } = useTranslation('dashboard');
  const user = useAuthStore((state) => state.userInfo);
  return (
    <ContentContainer heightMode="auto" scrollable padding="24px">
      <Card>
        <Space orientation="vertical" size="middle">
          <Typography.Title level={3} style={{ margin: 0 }}>
            {t('welcome', { name: user?.realname || user?.username || '' })}
          </Typography.Title>
          <Typography.Paragraph type="secondary" style={{ margin: 0 }}>
            {t('description')}
          </Typography.Paragraph>
        </Space>
      </Card>
    </ContentContainer>
  );
}
