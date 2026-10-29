import { useCallback, useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import type { Role } from './api';
import { auditFilterURL, emptyAuditFilters, getAudit, initialAuditFilters, listAudits, validAuditFilters } from './audit-api';
import type { AuditEntry, AuditFilters } from './audit-api';

const roleNames: Record<Role, string> = { viewer: '只读用户', operator: '操作员', admin: '管理员' };
const actionNames: Record<string, string> = {
  'auth.provision': '开通身份', 'auth.login': '登录', 'auth.login_denied': '登录被拒绝', 'auth.logout': '注销', 'auth.session_evicted': '退役旧会话',
  'strategy.create': '创建策略', 'strategy.publish': '发布配置', 'strategy.start': '启动策略', 'strategy.pause': '暂停策略', 'strategy.resume': '恢复策略', 'strategy.stop': '停止策略',
};
function actionName(value: string): string { return Object.hasOwn(actionNames, value) ? actionNames[value] : value; }
function auditTime(value: string): string {
  // Only integer timestamps are converted for display; IDs and revisions stay strings.
  const milliseconds = Number(value); const date = new Date(milliseconds);
  return Number.isSafeInteger(milliseconds) && !Number.isNaN(date.getTime()) ? `${date.toLocaleString('zh-CN', { hour12: false, timeZone: 'UTC' })} UTC` : value;
}
function useAuditQuery<T>(load: ((signal: AbortSignal) => Promise<T>) | null, onExpired: () => void) {
  const [data, setData] = useState<T | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [revision, setRevision] = useState(0);
  useEffect(() => {
    setData(null); setError('');
    if (!load) { setLoading(false); setError('筛选条件无效，请清空后重新查询。'); return; }
    let active = true; let timedOut = false; const controller = new AbortController();
    const timer = setTimeout(() => { timedOut = true; controller.abort(); }, 6000); setLoading(true);
    load(controller.signal).then(value => { if (active) setData(value); }).catch(cause => {
      if (!active) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      const code = cause instanceof ApiError ? cause.code : '';
      setError(timedOut ? '读取超时，请重试。' : code === 'NOT_FOUND' ? '未找到此审计记录。' : code === 'FORBIDDEN' ? '当前账户无权读取审计记录。' : '暂时无法读取审计记录，请稍后重试。');
    }).finally(() => { clearTimeout(timer); if (active) setLoading(false); });
    return () => { active = false; clearTimeout(timer); controller.abort(); };
  }, [load, onExpired, revision]);
  return { data, error, loading, reload: () => setRevision(value => value + 1) };
}
function AuditQueryState({ loading, error, retry }: { loading: boolean; error: string; retry: () => void }) {
  if (loading) return <div className="card loading" role="status"><span className="spinner" />正在读取审计记录…</div>;
  if (error) return <div className="card"><p className="error" role="alert">{error}</p><button className="button secondary" onClick={retry}>重试</button></div>;
  return null;
}
function Resource({ resource }: { resource: string }) {
  const match = /^strategy_instance:([1-9][0-9]{0,18})$/.exec(resource);
  const id = match?.[1];
  return id && BigInt(id) <= 9223372036854775807n ? <a className="detail-link audit-text" href={`/strategies/${id}`}>{resource}</a> : <span className="audit-text">{resource}</span>;
}
export function AuditWorkspace({ onExpired }: { onExpired: () => void }) {
  const id = /^\/audits\/([1-9][0-9]{0,18})$/.exec(location.pathname)?.[1];
  return id ? <AuditDetail id={id} onExpired={onExpired} /> : <AuditList onExpired={onExpired} />;
}
function AuditList({ onExpired }: { onExpired: () => void }) {
  const [filters, setFilters] = useState(initialAuditFilters); const [draft, setDraft] = useState<AuditFilters>(() => filters ?? emptyAuditFilters); const [validation, setValidation] = useState(''); const [cursors, setCursors] = useState(['0']);
  const cursor = cursors[cursors.length - 1];
  const loader = useCallback((signal: AbortSignal) => listAudits(filters ?? emptyAuditFilters, cursor, signal), [filters, cursor]);
  const query = useAuditQuery(filters ? loader : null, onExpired);
  function apply(next: AuditFilters) {
    if (!validAuditFilters(next)) { setValidation('筛选条件不能包含前后空白或控制字符，并须符合字段字节限制。'); return; }
    setValidation(''); setDraft(next); setFilters(next); setCursors(['0']); query.reload();
    const parameters = auditFilterURL(next); history.replaceState(null, '', `/audits${parameters ? `?${parameters}` : ''}`);
  }
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); apply({ ...draft }); }
  return <div className="strategy-workspace audit-workspace" aria-busy={query.loading}><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">AUDIT HISTORY</span><h2>审计记录</h2><p>核对管理操作的对象、原因与修订历史。</p></div><button className="button secondary compact" onClick={query.reload} disabled={query.loading}>刷新</button></div>
    <form className="card audit-filter-card" aria-label="审计筛选" onSubmit={submit}><div className="audit-filters">
      <label>操作者（精确）<input aria-label="操作者（精确）" maxLength={64} value={draft.actor} onChange={event => setDraft({ ...draft, actor: event.target.value })} /></label>
      <label>操作标识（精确）<input aria-label="操作标识（精确）" maxLength={64} value={draft.action} placeholder="例如 strategy.publish" onChange={event => setDraft({ ...draft, action: event.target.value })} /></label>
      <label>操作对象（精确）<input aria-label="操作对象（精确）" maxLength={128} value={draft.resource} placeholder="例如 strategy_instance:1" onChange={event => setDraft({ ...draft, resource: event.target.value })} /></label>
    </div><p className="muted">按原值精确匹配，不支持通配符。修改条件后点击查询。</p>{validation && <p className="error" role="alert">{validation}</p>}<div className="audit-filter-actions"><button className="button primary" type="submit">查询记录</button><button className="button secondary" type="button" onClick={() => apply({ ...emptyAuditFilters })}>清空筛选</button></div></form>
    <AuditQueryState loading={query.loading} error={query.error} retry={query.reload} />
    {query.data && <><div className="card data-card"><div className="card-heading"><h3>历史列表</h3><span className="badge">本页 {query.data.audits.length} 条</span></div><p className="notice audit-notice">审计记录保留提交事实；操作是否完成请另查运行状态与操作结果。</p>
      {query.data.audits.length === 0 ? <div className="empty-state"><h3>没有匹配的审计记录</h3><p>可调整精确筛选条件后重新查询。</p></div> : <div className="table-scroll" role="region" aria-label="审计列表" tabIndex={0}><table><thead><tr><th>时间 / ID</th><th>操作 / 操作者</th><th>对象 / 原因</th><th>前后修订</th><th><span className="sr-only">详情</span></th></tr></thead><tbody>{query.data.audits.map(entry => <tr key={entry.id}>
        <td>{auditTime(entry.created_at_ms)}<small>{entry.id}</small></td><td>{actionName(entry.action)}<small>{entry.action}</small><small>{entry.actor} · {roleNames[entry.actor_role]}</small></td><td className="audit-description"><Resource resource={entry.resource} /><small className="audit-text">{entry.reason}</small></td><td>{entry.before_version ?? '—'} → {entry.after_version ?? '—'}</td><td><a className="detail-link" href={`/audits/${entry.id}`} aria-label={`查看审计 ${entry.id}`}>查看 ↗</a></td>
      </tr>)}</tbody></table></div>}</div><nav className="pagination" aria-label="审计分页"><button className="button secondary" disabled={query.loading || cursors.length === 1} onClick={() => setCursors(cursors.slice(0, -1))}>上一页</button><span>第 {cursors.length} 页</span><button className="button secondary" disabled={query.loading || query.data.next_after_id === '0'} onClick={() => setCursors([...cursors, query.data!.next_after_id])}>下一页</button></nav></>}
  </div>;
}
function AuditDetail({ id, onExpired }: { id: string; onExpired: () => void }) {
  const loader = useCallback((signal: AbortSignal) => getAudit(id, signal), [id]); const query = useAuditQuery(loader, onExpired); const entry: AuditEntry | null = query.data;
  return <div className="strategy-workspace audit-workspace" aria-busy={query.loading}><a className="back-link" href="/audits">← 审计记录</a><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">AUDIT RECORD</span><h2>审计详情</h2><p>历史提交记录不代表当前权限或实际执行完成。</p></div><button className="button secondary compact" onClick={query.reload} disabled={query.loading}>刷新</button></div><AuditQueryState loading={query.loading} error={query.error} retry={query.reload} />
    {entry && <section className="card" aria-label="审计详情"><div className="card-heading"><h3>{actionName(entry.action)}</h3><span className="badge">历史事实</span></div><dl className="parameter-details"><div><dt>审计 ID</dt><dd>{entry.id}</dd></div><div><dt>操作者</dt><dd>{entry.actor}</dd></div><div><dt>当时角色</dt><dd>{roleNames[entry.actor_role]}</dd></div><div><dt>操作标识</dt><dd>{entry.action}</dd></div><div><dt>操作对象</dt><dd><Resource resource={entry.resource} /></dd></div><div><dt>变更原因</dt><dd className="audit-text">{entry.reason}</dd></div><div><dt>变更前修订</dt><dd>{entry.before_version ?? '—'}</dd></div><div><dt>变更后修订</dt><dd>{entry.after_version ?? '—'}</dd></div><div><dt>记录时间</dt><dd>{auditTime(entry.created_at_ms)}</dd></div><div><dt>UTC 毫秒</dt><dd>{entry.created_at_ms}</dd></div></dl></section>}
  </div>;
}
