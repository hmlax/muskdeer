import { useRef } from 'react';
import { Alert, App, Descriptions } from 'antd';
import { DrawerForm, ProFormDigit, ProFormText, type ProFormInstance } from '@ant-design/pro-components';
import { useTranslation } from 'react-i18next';
import type { MuskdeerAccount, MuskdeerAccountParameters } from '@/api/generated/admin/v1';
import { updateMuskdeerAccount, useMuskdeerWrite } from '@/api/hooks/muskdeer';
import { accountPolicyMessageKey } from '@/api/hooks/muskdeer-errors';
import { useDrawerCloseGuard } from '@/components/common/use-drawer-close-guard';

export default function AccountParametersForm({ current, onClose, onSaved }: {
  current: MuskdeerAccount;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation('muskdeer');
  const { message } = App.useApp();
  const formRef = useRef<ProFormInstance<MuskdeerAccountParameters>>(undefined);
  const save = useMuskdeerWrite(updateMuskdeerAccount);
  const { guardClose, handleValuesChange } = useDrawerCloseGuard(true, onClose);
  const required = [{ required: true, message: t('required') }];
  const decimal = [...required, { pattern: /^(?!0(?:\.0+)?$)(0|[1-9]\d*)(\.\d+)?$/, message: t('decimalRequired') }];
  return <DrawerForm<MuskdeerAccountParameters> open title={`${t('editAccountParameters')} · ${current.symbol ?? ''}`} formRef={formRef}
    initialValues={{ maximumNotional: current.maximumNotional, maximumOrderNotional: current.maximumOrderNotional, leverage: current.leverage }}
    onValuesChange={handleValuesChange} onOpenChange={(open) => {
      if (!open) guardClose();
    }}
    drawerProps={{ destroyOnHidden: true, maskClosable: false, onClose: guardClose }}
    submitter={{ searchConfig: { submitText: t('save'), resetText: t('close') }, resetButtonProps: { onClick: guardClose }, submitButtonProps: { loading: save.isPending } }}
    onFinish={async (parameters) => {
      try {
        await save.mutateAsync({ id: current.id, expectedUpdatedAtMs: current.updatedAtMs, parameters });
        message.success(t('accountParametersSaved'));
        onSaved();
        onClose();
        return true;
      } catch (error) {
        console.error('Update trading account parameters failed', error);
        message.error(t(accountPolicyMessageKey(error)));
        return false;
      }
    }}>
    <Alert type="info" title={t('accountParametersNotice')} style={{ marginBottom: 16 }} />
    <Descriptions bordered column={1} size="small" style={{ marginBottom: 16 }} items={[
      { key: 'account', label: t('accountId'), children: current.accountId },
      { key: 'user', label: t('userId'), children: current.userId },
      { key: 'fund', label: t('fundId'), children: current.fundId },
      { key: 'symbol', label: t('symbol'), children: current.symbol },
      { key: 'enabled', label: t('enabled'), children: t(current.enabled ? 'yes' : 'no') },
    ]} />
    <ProFormText name="maximumNotional" label={t('maximumNotional')} rules={decimal} />
    <ProFormText name="maximumOrderNotional" label={t('maximumOrderNotional')} rules={decimal} />
    <ProFormDigit name="leverage" label={t('leverage')} rules={required} fieldProps={{ min: 1, max: 10, precision: 0 }} />
  </DrawerForm>;
}
