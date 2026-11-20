import { Splitter } from 'antd';
import { useState } from 'react';

import ContentContainer from '@/layouts/components/PageContainer/ContentContainer';
import OrgList from './OrgList';
import UserList from './UserList';
import { useApiAccess } from '@/core/access';

/**
 * 用户管理页面
 * 使用 Splitter 实现左右分栏布局：左侧组织树，右侧用户列表
 */
const UserManagement = () => {
  const [currentTenantId, setCurrentTenantId] = useState<number | undefined>(undefined);
  const [currentOrgUnitId, setCurrentOrgUnitId] = useState<number | undefined>(undefined);
  const hasApiAccess = useApiAccess();
  const canReadOrganizations = hasApiAccess('GET', '/admin/v1/org-units');

  return (
    <ContentContainer heightMode="fixed" padding="16px" bottomMargin={0}>
      <Splitter style={{ height: '100%', flex: 1, minHeight: 0 }}>
        {canReadOrganizations && <Splitter.Panel collapsible defaultSize="25%" min="15%" max="40%" style={{ display: 'flex', flexDirection: 'column' }}>
          <OrgList
            currentOrgUnitId={currentOrgUnitId}
            currentTenantId={currentTenantId}
            onTenantSelect={setCurrentTenantId}
            onOrgSelect={setCurrentOrgUnitId}
          />
        </Splitter.Panel>}
        <Splitter.Panel style={{ display: 'flex', flexDirection: 'column' }}>
          <UserList tenantId={currentTenantId} orgUnitId={currentOrgUnitId} />
        </Splitter.Panel>
      </Splitter>
    </ContentContainer>
  );
};

export default UserManagement;
