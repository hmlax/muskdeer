import type { SelectionNode } from '@/types/selection';
import { useRef, useState, useEffect } from 'react';
import type { ProFormInstance } from '@ant-design/pro-components';
import {
  DrawerForm,
  ProFormText,
  ProFormDigit,
  ProFormTextArea,
  ProFormSelect,
  ProFormRadio,
  ProFormSwitch,
} from '@ant-design/pro-components';
import { App, TreeSelect, Form } from 'antd';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { OrgUnit as OrgUnit } from '@/api/generated/admin/v1';
import { PaginationQuery } from '@/core';
import { useCreateOrgUnit, useUpdateOrgUnit, fetchListOrgUnits } from '@/api/hooks/org-unit';
import { fetchListUsers } from '@/api/hooks/user';
import { useDrawerCloseGuard } from '@/components/common/use-drawer-close-guard';
import { getStatusOptions, getOrgTypeOptions } from '../constants';

interface OrgDrawerProps {
  open: boolean;
  mode: 'create' | 'edit';
  data?: OrgUnit;
  onClose: () => void;
  onSuccess: () => void;
}

/**
 * 组织架构编辑/创建抽屉组件
 */
const OrgDrawer: React.FC<OrgDrawerProps> = ({ open, mode, data, onClose, onSuccess }) => {
  const { t } = useTranslation('org-unit');
  const formRef = useRef<ProFormInstance>(null);

  // 关闭守卫：遮罩/ESC/X/取消统一，脏数据先确认（docs/form_drawer_close_guard.md）
  const { guardClose, handleValuesChange } = useDrawerCloseGuard(open, onClose);
  const queryClient = useQueryClient();
  const { message } = App.useApp();

  const [confirmLoading, setConfirmLoading] = useState(false);
  const [parentTreeData, setParentTreeData] = useState<SelectionNode[]>([]);
  const [leaderOptions, setLeaderOptions] = useState<{ label: string; value: number }[]>([]);

  // 加载上级组织树和用户列表
  useEffect(() => {
    if (open) {
      // 加载上级组织树
      fetchListOrgUnits(new PaginationQuery({ formValues: { status: 'ON' } }))
        .then((res) => {
          const items = res?.items || [];
          // 构建树形选择数据
          const treeData = buildOrgTreeSelect(items, data?.id);
          setParentTreeData(treeData);
        })
        .catch((error: unknown) => {
          console.error('Load options failed:', error);
          setParentTreeData([]);
        });

      // 加载用户列表（负责人）
      fetchListUsers(new PaginationQuery({}))
        .then((res) => {
          const items = res?.items || [];
          setLeaderOptions(
            items.map((u) => ({
              label: u.nickname || u.username || String(u.id),
              value: u.id as number,
            })),
          );
        })
        .catch((error: unknown) => {
          console.error('Load options failed:', error);
          setLeaderOptions([]);
        });
    }
  }, [open, data?.id]);

  // 编辑模式填充表单
  useEffect(() => {
    if (open && mode === 'edit' && data) {
      // 使用 setTimeout 确保 DrawerForm 内部初始化完成后再设值
      setTimeout(() => {
        formRef.current?.setFieldsValue({
          name: data.name || '',
          code: data.code || '',
          parentId: data.parentId || undefined,
          leaderId: data.leaderId || undefined,
          type: data.type || undefined,
          sortOrder: data.sortOrder ?? 1,
          status: data.status || 'ON',
          isLegalEntity: data.isLegalEntity ?? false,
          registrationNumber: data.registrationNumber || '',
          taxId: data.taxId || '',
          address: data.address || '',
          description: data.description || '',
          remark: data.remark || '',
        });
      }, 0);
    }
  }, [open, mode, data]);

  const createMutation = useCreateOrgUnit({
    onSuccess: () => {
      message.success(t('createSuccess'));
      onSuccess();
      onClose();
      queryClient.invalidateQueries({ queryKey: ['listOrgUnits'] });
    },
    onError: (error: Error) => {
      message.error(error.message || t('createFailed'));
    },
  });

  const updateMutation = useUpdateOrgUnit({
    onSuccess: () => {
      message.success(t('updateSuccess'));
      onSuccess();
      onClose();
      queryClient.invalidateQueries({ queryKey: ['listOrgUnits'] });
    },
    onError: (error: Error) => {
      message.error(error.message || t('updateFailed'));
    },
  });

  const handleSubmit = async (values: OrgUnit) => {
    setConfirmLoading(true);
    try {
      if (mode === 'create') {
        await createMutation.mutateAsync({ data: values });
      } else if (data?.id) {
        await updateMutation.mutateAsync({ id: data.id, values });
      }
    } finally {
      setConfirmLoading(false);
    }
  };

  return (
    <DrawerForm
      formRef={formRef}
      title={mode === 'create' ? t('create') : t('edit')}
      open={open}
      onValuesChange={handleValuesChange}
      initialValues={{
        sortOrder: 1,
        status: 'ON',
        isLegalEntity: false,
      }}
      onFinish={handleSubmit}
      submitter={{
        searchConfig: {
          submitText: t('common:button.submit'),
          resetText: t('common:button.cancel'),
        },
        submitButtonProps: {
          loading: confirmLoading || createMutation.isPending || updateMutation.isPending,
        },
        resetButtonProps: { onClick: guardClose },
      }}
      drawerProps={{ destroyOnHidden: true, onClose: guardClose, size: 600 }}
      layout="horizontal"
      labelCol={{ flex: '0 0 120px' }}
      wrapperCol={{ flex: 'auto' }}
    >
      <ProFormText
        name="name"
        label={t('name')}
        placeholder={t('namePlaceholder')}
        rules={[{ required: true, message: t('requiredName') }]}
        fieldProps={{ allowClear: true }}
      />

      <ProFormText
        name="code"
        label={t('code')}
        placeholder={t('codePlaceholder')}
        rules={[{ required: true, message: t('requiredCode') }]}
        fieldProps={{ allowClear: true }}
      />

      <Form.Item name="parentId" label={t('parentId')}>
        <TreeSelect
          showSearch
          allowClear
          treeDefaultExpandAll
          placeholder={t('parentIdPlaceholder')}
          treeData={parentTreeData}
        />
      </Form.Item>

      <ProFormSelect
        name="leaderId"
        label={t('leaderId')}
        placeholder={t('leaderIdPlaceholder')}
        options={leaderOptions}
        fieldProps={{
          showSearch: true,
          allowClear: true,
          filterOption: (input, option) =>
            String(option?.label ?? '')
              .toLowerCase()
              .includes(input.toLowerCase()),
        }}
      />

      <ProFormSelect
        name="type"
        label={t('type')}
        placeholder={t('typePlaceholder')}
        options={getOrgTypeOptions(t)}
        rules={[{ required: true, message: t('requiredType') }]}
        fieldProps={{
          showSearch: true,
          allowClear: true,
          filterOption: (input, option) =>
            String(option?.label ?? '')
              .toLowerCase()
              .includes(input.toLowerCase()),
        }}
      />

      <ProFormDigit
        name="sortOrder"
        label={t('sortOrder')}
        placeholder={t('sortOrderPlaceholder')}
        rules={[{ required: true }]}
        fieldProps={{ precision: 0, min: 0 }}
      />

      <ProFormRadio.Group
        name="status"
        label={t('status')}
        rules={[{ required: true }]}
        options={getStatusOptions(t)}
        fieldProps={{ optionType: 'button', buttonStyle: 'solid' }}
      />

      <ProFormSwitch name="isLegalEntity" label={t('isLegalEntity')} />

      <ProFormText
        name="registrationNumber"
        label={t('registrationNumber')}
        placeholder={t('registrationNumberPlaceholder')}
        fieldProps={{ allowClear: true }}
      />

      <ProFormText
        name="taxId"
        label={t('taxId')}
        placeholder={t('taxIdPlaceholder')}
        fieldProps={{ allowClear: true }}
      />

      <ProFormText
        name="address"
        label={t('address')}
        placeholder={t('addressPlaceholder')}
        fieldProps={{ allowClear: true }}
      />

      <ProFormTextArea
        name="description"
        label={t('description')}
        placeholder={t('descriptionPlaceholder')}
        fieldProps={{ allowClear: true, rows: 2 }}
      />

      <ProFormTextArea
        name="remark"
        label={t('remark')}
        placeholder={t('remarkPlaceholder')}
        fieldProps={{ allowClear: true, rows: 2 }}
      />
    </DrawerForm>
  );
};

/**
 * 将 API 返回的树形组织数据转为 TreeSelect 格式，
 * 并排除当前编辑节点及其子节点，防止循环引用
 */
function buildOrgTreeSelect(items: OrgUnit[], excludeId?: number | string): SelectionNode[] {
  const result: SelectionNode[] = [];

  for (const item of items) {
    if (item.id === undefined) continue;
    // 排除当前编辑节点
    if (excludeId && item.id === Number(excludeId)) continue;

    const node: SelectionNode = {
      id: item.id,
      key: item.id,
      value: item.id,
      title: item.name || String(item.id),
      label: item.name || String(item.id),
    };

    if (item.children && item.children.length > 0) {
      const children = buildOrgTreeSelect(item.children, excludeId);
      if (children.length > 0) {
        node.children = children;
      }
    }

    result.push(node);
  }

  return result;
}

export default OrgDrawer;
