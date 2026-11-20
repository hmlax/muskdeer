import { useRef } from 'react';
import type { ReactNode } from 'react';
import type { ActionType, ProColumns } from '@ant-design/pro-components';
import { useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert } from 'antd';
import ListTable from '@/components/common/ListTable';
import ContentContainer from '@/layouts/components/PageContainer/ContentContainer';
import { useMuskdeerScope } from '@/api/hooks/muskdeer';

export type ListQuery = { page?: number; pageSize?: number; search?: string; symbol?: string; accountId?: string; state?: string; runId?: string; strategyType?: string; instanceId?: number; tenantId?: number };
type Props<T extends object> = {
 resource: string;
 rowKey?: string;
 columns: ProColumns<T>[];
 fetch: (query: ListQuery) => Promise<{ items?: T[]; total?: number }>;
 toolbar?: ReactNode[];
 actionRef?: React.RefObject<ActionType | null>;
 notice?: string;
};
export default function ResourceTable<T extends object>({ resource, rowKey = 'id', columns, fetch, toolbar, actionRef, notice }: Props<T>) {
 const localRef = useRef<ActionType>(null);
 const client = useQueryClient();
 const scope = useMuskdeerScope();
 const { t } = useTranslation('muskdeer');
 return <ContentContainer heightMode="fixed" padding="16px" bottomMargin={0}>
  {notice && <Alert type="info" title={notice} style={{ marginBottom: 12 }} />}
  <ListTable<T> rowKey={rowKey} actionRef={actionRef ?? localRef} headerTitle={t(resource)} columns={columns}
   scroll={{ x: 'max-content' }} pagination={{ defaultPageSize: 20, pageSizeOptions: [20, 50, 100, 200], showSizeChanger: true }}
   toolBarRender={() => toolbar ?? []}
   request={async (params) => {
    const query: ListQuery = { ...params, page: params.current ?? 1, pageSize: params.pageSize ?? 20 };
    const response = await client.fetchQuery({ queryKey: ['muskdeer', scope, resource, query], queryFn: () => fetch(query), retry: 0, staleTime: 0 });
    return { data: response.items ?? [], total: response.total ?? 0, success: true };
   }} />
 </ContentContainer>;
}
