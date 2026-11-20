import { useRef, useState } from 'react';
import { App, Button, Tag } from 'antd';
import { DrawerForm, ProFormDigit, ProFormSwitch, ProFormText } from '@ant-design/pro-components';
import type { ActionType, ProColumns, ProFormInstance } from '@ant-design/pro-components';
import { useTranslation } from 'react-i18next';
import type { MuskdeerAccountBinding, MuskdeerBindingDraft } from '@/api/generated/admin/v1';
import { createMuskdeerBinding, muskdeerReaders, setMuskdeerBindingEnabled, useMuskdeerWrite } from '@/api/hooks/muskdeer';
import { useAccess } from '@/core/access';
import { useDrawerCloseGuard } from '@/components/common/use-drawer-close-guard';
import ResourceTable from '../components/ResourceTable';

export default function Bindings() {
  const { t } = useTranslation('muskdeer');
  const { message, modal } = App.useApp();
  const { hasAccessByCodes } = useAccess();
  const actionRef = useRef<ActionType>(null);
  const formRef = useRef<ProFormInstance<MuskdeerBindingDraft>>(null);
  const [open, setOpen] = useState(false);
  const create = useMuskdeerWrite(createMuskdeerBinding);
  const update = useMuskdeerWrite(setMuskdeerBindingEnabled);
  const { guardClose, handleValuesChange } = useDrawerCloseGuard(open, () => setOpen(false));
  const columns: ProColumns<MuskdeerAccountBinding>[] = [
    { title: t('id'), dataIndex: 'id', hideInSearch: true },
    { title: t('tenantId'), dataIndex: 'tenantId', valueType: 'digit' },
    { title: t('accountId'), dataIndex: 'accountId' },
    { title: t('version'), dataIndex: 'version', hideInSearch: true },
    { title: t('enabled'), hideInSearch: true, render: (_, row) => <Tag color={row.enabled ? 'green' : 'default'}>{t(row.enabled ? 'yes' : 'no')}</Tag> },
    { title: t('actions'), valueType: 'option', render: (_, row) => hasAccessByCodes(['muskdeer:binding:update', 'sys:platform_admin']) && <Button type="link" disabled={update.isPending} onClick={() => modal.confirm({
      title: t('confirmBinding'), onOk: async () => {
        try { await update.mutateAsync({ id: row.id, expectedVersion: row.version, enabled: !row.enabled }); message.success(t('saved')); }
        catch (error) { console.error('Update binding failed', error); message.error(t('failed')); }
        finally { await actionRef.current?.reload(); }
      },
    })}>{t(row.enabled ? 'disable' : 'enable')}</Button> },
  ];
  return <>
    <ResourceTable<MuskdeerAccountBinding> resource="account-bindings" notice={t('bindingNotice')} columns={columns} fetch={muskdeerReaders.bindings} actionRef={actionRef}
      toolbar={hasAccessByCodes(['muskdeer:binding:create', 'sys:platform_admin']) ? [<Button key="create" type="primary" onClick={() => setOpen(true)}>{t('create')}</Button>] : []} />
    <DrawerForm<MuskdeerBindingDraft> open={open} formRef={formRef} title={t('create')} initialValues={{ enabled: true }} onValuesChange={handleValuesChange}
      drawerProps={{ size: 480, destroyOnHidden: true, onClose: guardClose }}
      submitter={{ resetButtonProps: { onClick: guardClose }, submitButtonProps: { loading: create.isPending } }}
      onFinish={async (values) => {
        try { await create.mutateAsync(values); message.success(t('saved')); setOpen(false); await actionRef.current?.reload(); return true; }
        catch (error) { console.error('Create binding failed', error); message.error(t('failed')); return false; }
      }}>
      <ProFormDigit name="tenantId" label={t('tenantId')} fieldProps={{ min: 1, precision: 0 }} rules={[{ required: true }]} />
      <ProFormText name="accountId" label={t('accountId')} rules={[{ required: true }]} />
      <ProFormSwitch name="enabled" label={t('enabled')} />
    </DrawerForm>
  </>;
}
