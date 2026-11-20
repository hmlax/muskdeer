import type { OrgUnit } from '@/api/generated/admin/v1';
import { getErrorMessage } from '@/utils/error';
import { useState, useEffect, useMemo } from 'react';
import { Tree, Select, Input, Button, App, theme } from 'antd';
import {
  ExpandOutlined,
  ShrinkOutlined,
  CloseCircleOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import { useTranslation } from 'react-i18next';

import { PaginationQuery } from '@/core';
import { useListOrgUnits } from '@/api/hooks/org-unit';
import { useListTenants } from '@/api/hooks/tenant';
import { useUserStore } from '@/stores/user';

interface OrgTreeNode {
  id: number;
  key: number;
  title: string;
  children?: OrgTreeNode[];
}

interface OrgListProps {
  currentOrgUnitId: number | undefined;
  currentTenantId: number | undefined;
  onTenantSelect: (tenantId: number | undefined) => void;
  onOrgSelect: (orgUnitId: number | undefined) => void;
}

/**
 * 左侧组织树列表
 * 包含租户选择器（非租户用户可见）、搜索、树操作
 */
const OrgList: React.FC<OrgListProps> = ({
  currentOrgUnitId,
  currentTenantId,
  onTenantSelect,
  onOrgSelect,
}) => {
  const { t } = useTranslation('user');
  const { message } = App.useApp();
  const isTenantUser = useUserStore((s) => s.isTenantUser());

  const tenantId = useUserStore((state) => state.tenantId);
  const effectiveTenantId = isTenantUser ? (tenantId ?? 0) : (currentTenantId ?? 0);
  const tenantParams = useMemo(() => new PaginationQuery({ formValues: { status: 'ON' } }), []);
  const tenantQuery = useListTenants(tenantParams, { enabled: !isTenantUser });
  const tenantOptions = (tenantQuery.error ? [] : (tenantQuery.data?.items ?? [])).flatMap(
    (item) =>
      item.id === undefined
        ? []
        : [{ label: item.name ?? item.code ?? String(item.id), value: item.id }],
  );
  const orgParams = useMemo(
    () =>
      new PaginationQuery({
        formValues: {
          ...(effectiveTenantId ? { tenant_id: effectiveTenantId } : {}),
          status: 'ON',
        },
      }),
    [effectiveTenantId],
  );
  const orgQuery = useListOrgUnits(orgParams);
  const treeData = useMemo(() => mapToAntTree(orgQuery.data?.items ?? []), [orgQuery.data]);
  useEffect(() => {
    const error = orgQuery.error ?? tenantQuery.error;
    if (error) {
      console.error('Failed to load organization selection:', error);
      message.error(getErrorMessage(error) || t('fetchFailed'));
    }
  }, [orgQuery.error, tenantQuery.error, message, t]);

  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);
  const [searchValue, setSearchValue] = useState('');
  const [autoExpandParent, setAutoExpandParent] = useState(true);
  const selectedKeys =
    currentOrgUnitId !== undefined && collectAllKeys(treeData).includes(currentOrgUnitId)
      ? [currentOrgUnitId]
      : [];

  const handleTenantChanged = (value: number | undefined) => {
    onTenantSelect(value);
    onOrgSelect(undefined);
    setSearchValue('');
    setExpandedKeys([]);
    setAutoExpandParent(false);
  };
  const handleSelect = (keys: React.Key[], info: { node: OrgTreeNode }) => {
    onOrgSelect(keys.length > 0 ? info.node.id : undefined);
  };
  const handleSearchChanged = (value: string) => {
    setSearchValue(value);
    const query = value.trim().toLowerCase();
    const parentKeys = new Set<React.Key>();
    if (query) collectMatchParents(treeData, query, parentKeys);
    setExpandedKeys([...parentKeys]);
    setAutoExpandParent(Boolean(query));
  };
  const handleExpandAll = () => {
    setExpandedKeys(collectAllKeys(treeData));
    setAutoExpandParent(true);
  };
  const handleCollapseAll = () => {
    setExpandedKeys([]);
    setAutoExpandParent(false);
  };
  const handleClearSelect = () => onOrgSelect(undefined);

  const { token } = theme.useToken();

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        overflow: 'hidden',
        borderRadius: token.borderRadiusLG,
        border: `1px solid ${token.colorBorderSecondary}`,
        backgroundColor: token.colorBgContainer,
      }}
    >
      {/* 头部标题栏 */}
      <div
        style={{
          flex: '0 0 auto',
          padding: '10px 12px 8px',
          borderBottom: `1px solid ${token.colorBorderSecondary}`,
        }}
      >
        <div
          style={{
            fontSize: 14,
            fontWeight: 600,
            color: token.colorTextHeading,
            marginBottom: 8,
          }}
        >
          {t('searchOrg')}
        </div>

        {/* 租户选择器（仅非租户用户可见） */}
        {!isTenantUser && (
          <Select
            showSearch
            allowClear
            style={{ width: '100%', marginBottom: 8 }}
            placeholder={t('tenantIdPlaceholder')}
            value={currentTenantId ?? undefined}
            options={tenantOptions}
            onChange={(value) => handleTenantChanged(value != null ? Number(value) : undefined)}
          />
        )}

        {/* 搜索栏 + 工具按钮 */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <Input
            allowClear
            prefix={<SearchOutlined style={{ color: token.colorTextQuaternary }} />}
            style={{ flex: 1 }}
            placeholder={t('searchOrg')}
            value={searchValue}
            onChange={(e) => handleSearchChanged(e.target.value)}
          />
          <Button
            type="text"
            size="small"
            icon={<ExpandOutlined />}
            onClick={handleExpandAll}
            title={t('expandAll')}
          />
          <Button
            type="text"
            size="small"
            icon={<ShrinkOutlined />}
            onClick={handleCollapseAll}
            title={t('collapseAll')}
          />
          <Button
            type="text"
            size="small"
            icon={<CloseCircleOutlined />}
            onClick={handleClearSelect}
            title={t('clearSelect')}
          />
        </div>
      </div>

      {/* 组织树 */}
      <div style={{ flex: 1, minHeight: 0, overflow: 'auto', padding: '4px 4px 8px' }}>
        <Tree<OrgTreeNode>
          blockNode
          expandedKeys={expandedKeys}
          autoExpandParent={autoExpandParent}
          selectedKeys={selectedKeys}
          treeData={treeData}
          onExpand={(keys) => {
            setExpandedKeys(keys);
            setAutoExpandParent(false);
          }}
          onSelect={handleSelect}
          titleRender={(nodeData) => {
            const title = nodeData.title as string;
            const q = searchValue.trim();
            if (!q || !title) return title;
            const idx = title.toLowerCase().indexOf(q.toLowerCase());
            if (idx === -1) return title;
            return (
              <span>
                {title.substring(0, idx)}
                <span style={{ color: token.colorPrimary }}>
                  {title.substring(idx, idx + q.length)}
                </span>
                {title.substring(idx + q.length)}
              </span>
            );
          }}
        />
      </div>
    </div>
  );
};

export default OrgList;

// ========== 工具函数 ==========

/**
 * 将 API 返回的树形 OrgUnit 数据映射为 antd Tree 所需的格式
 * API 已返回 children 树形结构，只需添加 key/title 并递归处理
 */
function mapToAntTree(items: OrgUnit[]): OrgTreeNode[] {
  return items.flatMap((item) => {
    if (item.id === undefined) return [];
    const children = mapToAntTree(item.children ?? []);
    return [
      {
        id: item.id,
        key: item.id,
        title: item.name ?? '',
        ...(children.length ? { children } : {}),
      },
    ];
  });
}

/**
 * 收集树所有节点 key
 */
function collectAllKeys(tree: OrgTreeNode[]): React.Key[] {
  const keys: React.Key[] = [];
  const walk = (nodes: OrgTreeNode[]) => {
    nodes.forEach((n) => {
      keys.push(n.key);
      if (n.children?.length) walk(n.children);
    });
  };
  walk(tree);
  return keys;
}

/**
 * 收集匹配搜索文字的父节点 key
 */
function collectMatchParents(
  tree: OrgTreeNode[],
  query: string,
  parentKeys: Set<React.Key>,
  parents: React.Key[] = [],
) {
  tree.forEach((node) => {
    const title = (node.title || '').toLowerCase();
    if (title.includes(query)) {
      parents.forEach((p) => parentKeys.add(p));
    }
    if (node.children?.length) {
      collectMatchParents(node.children, query, parentKeys, [...parents, node.key]);
    }
  });
}
