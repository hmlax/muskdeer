import { useRef } from 'react';
import { Alert, App } from 'antd';
import { DrawerForm, ProFormDigit, ProFormSelect, ProFormText } from '@ant-design/pro-components';
import type { ProFormInstance } from '@ant-design/pro-components';
import { useTranslation } from 'react-i18next';
import type { MuskdeerAccountDraft } from '@/api/generated/admin/v1';
import { createMuskdeerAccount, muskdeerReaders, useMuskdeerWrite } from '@/api/hooks/muskdeer';
import { useDrawerCloseGuard } from '@/components/common/use-drawer-close-guard';
import { loadAccountOptions, validTradingID } from './account-options';

type Props = { onClose: () => void; onSaved: () => void };

export default function AccountForm({ onClose, onSaved }: Props) {
  const { t } = useTranslation('muskdeer');
  const { message } = App.useApp();
  const formRef = useRef<ProFormInstance<MuskdeerAccountDraft>>(undefined);
  const save = useMuskdeerWrite(createMuskdeerAccount);
  const { guardClose, handleValuesChange } = useDrawerCloseGuard(true, onClose);
  const required = [{ required: true, message: t('required') }];
  const identifier = [...required, { validator: (_: unknown, value: unknown) => validTradingID(value) ? Promise.resolve() : Promise.reject(new Error(t('tradingIDRequired'))) }];
  const decimal = [...required, { pattern: /^(?!0(?:\.0+)?$)(0|[1-9]\d*)(\.\d+)?$/, message: t('decimalRequired') }];
  return <DrawerForm<MuskdeerAccountDraft> open title={t('addAccount')} formRef={formRef}
    initialValues={{ marketDataSource: 'binance', leverage: 10, maximumNotional: '1000', maximumOrderNotional: '100' }}
    onValuesChange={handleValuesChange} onOpenChange={(open) => { if (!open) guardClose(); }}
    drawerProps={{ destroyOnHidden: true, maskClosable: false, onClose: guardClose }}
    submitter={{ searchConfig: { submitText: t('save'), resetText: t('close') }, resetButtonProps: { onClick: guardClose }, submitButtonProps: { loading: save.isPending } }}
    onFinish={async (values) => {
      try {
        await save.mutateAsync(values);
        message.success(t('accountAdded')); onSaved(); onClose(); return true;
      } catch (error) { console.error('Add trading account failed', error); message.error(t('failed')); return false; }
    }}>
    <Alert type="info" title={t('accountEnrollmentNotice')} style={{ marginBottom: 16 }} />
    <ProFormSelect name="userId" label={t('userId')} rules={identifier} showSearch
      request={async () => [...new Set((await loadAccountOptions(muskdeerReaders.accounts)).map((row) => String(row.userId)))].filter(validTradingID).map((id) => ({ label: id, value: id }))} />
    <ProFormText name="fundId" label={t('fundId')} rules={identifier} />
    <ProFormSelect name="instrumentId" label={t('symbol')} rules={identifier} showSearch
      request={async () => (await loadAccountOptions(muskdeerReaders.instruments)).filter((row) => row.status === 'TRADING' && row.executionVenue === 'gateway').map((row) => ({ label: row.symbol, value: String(row.id) }))} />
    <ProFormSelect name="marketDataSource" label={t('marketDataSource')} rules={required} options={[{ label: 'Binance', value: 'binance' }]} />
    <ProFormDigit name="leverage" label={t('leverage')} rules={required} fieldProps={{ min: 1, max: 10, precision: 0 }} />
    <ProFormText name="maximumNotional" label={t('maximumNotional')} rules={decimal} />
    <ProFormText name="maximumOrderNotional" label={t('maximumOrderNotional')} rules={decimal} />
  </DrawerForm>;
}
