import { useCallback, useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import { integer } from './strategy-api';
import { getRiskEvent, listRiskEvents, riskEventKinds, riskEventReasons, riskEventSearch, validateRiskEventFilters } from './risk-event-api';
import type { RiskEvent, RiskEventFilters } from './risk-event-api';

const kinds = { BASELINE: '记录起点', CHANGED: '诊断变化', REMOVED: '源已移除' };
const reasons = { NOT_EVALUATED: '尚未评估', READY: '曾判为就绪', STRATEGY_STOPPED: '策略未运行', INVALID_PARAMETERS: '参数无效', BUDGET_DISABLED: '预算关闭', UNRECONCILED: '未完成对账', MARKET_UNAVAILABLE: '所选行情不可用', CONFIGURATION_PENDING: '配置待激活' };
const historicalNotice = '仅展示当时保存的诊断。许可位、READY 和记录期限不代表当前风险许可；读取不会补账或恢复交易。';
function useRiskEventRead<T>(load: (signal: AbortSignal) => Promise<T>, onExpired: () => void) {
  const [data, setData] = useState<T | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [revision, setRevision] = useState(0);
  useEffect(() => {
    setData(null); setError(''); setLoading(true); let active = true; let timeout = false; const controller = new AbortController();
    const timer = setTimeout(() => { timeout = true; controller.abort(); }, 6000);
    load(controller.signal).then(value => { if (active) setData(value); }).catch(cause => {
      if (!active) return; const code = cause instanceof ApiError ? cause.code : '';
      if (code === 'UNAUTHENTICATED') { onExpired(); return; }
      setError(timeout ? '风险历史读取超时，请重试。' : code === 'NOT_FOUND' ? '未找到此风险历史记录。' : code === 'FORBIDDEN' ? '当前账户无权查看风险历史。' : '暂时无法读取风险历史，请稍后重试。');
    }).finally(() => { clearTimeout(timer); if (active) setLoading(false); });
    return () => { active = false; clearTimeout(timer); controller.abort(); };
  }, [load, onExpired, revision]);
  return { data, error, loading, reload: () => setRevision(value => value + 1) };
}
function QueryStatus({ loading, error, retry }: { loading: boolean; error: string; retry: () => void }) {
  return loading ? <p role="status">正在读取风险历史…</p> : error ? <div><p className="error" role="alert">{error}</p><button className="button secondary" onClick={retry}>重试风险历史读取</button></div> : null;
}
function utc(value: string) { const milliseconds = Number(value); const date = new Date(milliseconds); return Number.isSafeInteger(milliseconds) && !Number.isNaN(date.getTime()) ? date.toISOString() : value; }
function Fields({ values }: { values: [string, string][] }) { return <dl className="parameter-details">{values.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>; }
function EventDetail({ value }: { value: RiskEvent }) {
  return <><section className="card risk-event-card" aria-label="风险历史详情"><div className="card-heading"><h3>{kinds[value.kind]} · {value.id}</h3><span className="badge">历史诊断</span></div><Fields values={[
    ['事件 ID', value.id], ['记录类型', value.kind], ['记录时间（UTC）', utc(value.recorded_at_ms)], ['源快照 ID', value.snapshot_id], ['源快照修订', value.snapshot_revision], ['策略 ID', value.strategy_id], ['账户 ID', value.account_id], ['交易对 ID', value.instrument_id], ['记录配置版本', value.config_version], ['记录行情来源', value.market_data_source], ['源退役或移除', String(value.snapshot_deleted)],
  ]} /><a className="detail-link" href={`/strategies/${value.strategy_id}`}>查看策略 ↗</a></section><section className="card risk-event-card" aria-label="记录时的风险输入"><div className="card-heading"><h3>记录时的风险输入</h3><span className="badge">不授予当前风险</span></div><Fields values={[
    ['记录原因', value.reason_code], ['原因含义', reasons[value.reason_code]], ['记录许可位', String(value.recorded_allow_new_risk)], ['记录对账位', String(value.recorded_reconciled)], ['记录观察时间（UTC）', utc(value.observed_at_ms)], ['记录截止时间（UTC）', utc(value.expires_at_ms)],
  ]} /><p className="notice">{historicalNotice}</p></section><section className="card risk-event-card" aria-label="记录时的行情引用"><div className="card-heading"><h3>记录时的行情引用</h3></div><Fields values={[
    ['行情绑定 ID', value.market_feed_id ?? '未记录'], ['发布者代次', value.publisher_epoch], ['行情序列', value.market_sequence], ['发布修订', value.publication_revision], ['来源政策版本', value.source_version], ['记录最大行情年龄（毫秒）', value.market_maximum_age_ms],
  ]} /><p className="notice">这是事件保存的引用，不检查或承诺当前行情健康。源快照已移除时，历史仍可查询。</p></section></>;
}
function EventDetails({ id, onExpired }: { id: string; onExpired: () => void }) {
  const load = useCallback((signal: AbortSignal) => getRiskEvent(id, signal), [id]); const query = useRiskEventRead(load, onExpired);
  return <><a className="back-link" href={query.data ? `/risk-events?strategy_id=${query.data.strategy_id}` : '/risk-events'}>← 风险历史</a><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">RISK DIAGNOSTIC RECORD</span><h2>风险历史详情</h2><p>保留事件写入时的范围与原始值。</p></div><button className="button secondary compact" disabled={query.loading} onClick={query.reload}>刷新风险历史</button></div><QueryStatus loading={query.loading} error={query.error} retry={query.reload} />{query.data && <EventDetail value={query.data} />}</>;
}
function initialFilters(): RiskEventFilters {
  const values = new URLSearchParams(location.search);
  for (const key of values.keys()) if (!['strategy_id', 'account_id', 'instrument_id', 'market_data_source', 'kind', 'reason_code', 'limit'].includes(key) || values.getAll(key).length !== 1 || !values.get(key)) throw new Error();
  const value = { strategy: values.get('strategy_id') ?? '', account: values.get('account_id') ?? '', instrument: values.get('instrument_id') ?? '', source: values.get('market_data_source') ?? '', kind: values.get('kind') ?? '', reason: values.get('reason_code') ?? '', limit: 20 };
  const limit = values.get('limit'); if (limit) { if (!['1', '20', '50'].includes(limit)) throw new Error(); value.limit = Number(limit); }
  validateRiskEventFilters(value); return value;
}
function EventList({ initial, onExpired }: { initial: RiskEventFilters; onExpired: () => void }) {
  const [draft, setDraft] = useState(initial); const [filters, setFilters] = useState(initial); const [formError, setFormError] = useState(''); const [cursors, setCursors] = useState(['0']); const cursor = cursors[cursors.length - 1];
  const load = useCallback((signal: AbortSignal) => listRiskEvents(cursor, filters, signal), [cursor, filters]); const query = useRiskEventRead(load, onExpired);
  function apply(event: FormEvent) {
    event.preventDefault(); let values: URLSearchParams;
    try { values = riskEventSearch(draft); } catch { setFormError('请输入规范账户 ID 和正整数策略、交易对 ID，或留空查询全部。'); return; }
    setFormError(''); setFilters({ ...draft }); setCursors(['0']); history.replaceState(null, '', `/risk-events${values.size ? `?${values}` : ''}`);
  }
  return <><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">RISK DIAGNOSTIC HISTORY</span><h2>风险历史</h2><p>从真实记录起点追踪诊断变化。</p></div><button className="button secondary compact" disabled={query.loading} onClick={query.reload}>刷新风险历史</button></div><section className="card risk-event-card" aria-label="风险历史查询"><p className="notice">{historicalNotice}</p><form className="risk-event-filters" onSubmit={apply}>
    <label>策略 ID<input aria-label="策略 ID" inputMode="numeric" maxLength={19} value={draft.strategy} onChange={event => setDraft({ ...draft, strategy: event.target.value })} placeholder="留空查询全部" /></label><label>账户 ID<input aria-label="账户 ID" maxLength={64} value={draft.account} onChange={event => setDraft({ ...draft, account: event.target.value })} placeholder="留空查询全部" /></label><label>交易对 ID<input aria-label="交易对 ID" inputMode="numeric" maxLength={19} value={draft.instrument} onChange={event => setDraft({ ...draft, instrument: event.target.value })} placeholder="留空查询全部" /></label>
    <label>记录行情来源<select aria-label="记录行情来源" value={draft.source} onChange={event => setDraft({ ...draft, source: event.target.value })}><option value="">全部来源</option><option value="binance">Binance</option><option value="okx">OKX</option></select></label><label>记录类型<select aria-label="记录类型" value={draft.kind} onChange={event => setDraft({ ...draft, kind: event.target.value })}><option value="">全部类型</option>{riskEventKinds.map(kind => <option key={kind} value={kind}>{kinds[kind]} · {kind}</option>)}</select></label>
    <label>记录原因<select aria-label="记录原因" value={draft.reason} onChange={event => setDraft({ ...draft, reason: event.target.value })}><option value="">全部原因</option>{riskEventReasons.map(reason => <option key={reason} value={reason}>{reasons[reason]} · {reason}</option>)}</select></label><label>每页事件<select aria-label="每页事件" value={draft.limit} onChange={event => setDraft({ ...draft, limit: Number(event.target.value) })}>{[1, 20, 50].map(limit => <option key={limit}>{limit}</option>)}</select></label><button className="button secondary" type="submit">筛选风险历史</button>
    </form>{formError && <p className="error" role="alert">{formError}</p>}<QueryStatus loading={query.loading} error={query.error} retry={query.reload} />{query.data && <>{query.data.events.length === 0 ? <p className="empty-state">没有匹配的风险历史记录</p> : <div className="table-scroll" role="region" aria-label="风险历史列表" tabIndex={0}><table><thead><tr><th>记录 / 时间</th><th>策略 / 账户</th><th>来源 / 配置</th><th>记录原因 / 原始位</th><th>详情</th></tr></thead><tbody>{query.data.events.map(value => <tr key={value.id}><td>{kinds[value.kind]} · {value.id}<small>{utc(value.recorded_at_ms)}</small></td><td>策略 {value.strategy_id}<small>{value.account_id} · 产品 {value.instrument_id}</small></td><td>{value.market_data_source}<small>版本 {value.config_version}</small></td><td>{value.reason_code}<small>许可 {String(value.recorded_allow_new_risk)} · 对账 {String(value.recorded_reconciled)}</small></td><td><a className="detail-link" href={`/risk-events/${value.id}`} aria-label={`查看风险事件 ${value.id}`}>查看 ↗</a></td></tr>)}</tbody></table></div>}
    <nav className="pagination" aria-label="风险历史分页"><button className="button secondary" disabled={query.loading || cursors.length === 1} onClick={() => setCursors(cursors.slice(0, -1))}>上一页事件</button><span>第 {cursors.length} 页</span><button className="button secondary" disabled={query.loading || query.data.next_after_id === '0'} onClick={() => setCursors([...cursors, query.data!.next_after_id])}>下一页事件</button></nav><p className="notice">起点记录不代表更早历史完整。分页用于浏览；可刷新重新观察已提交记录。</p></>}</section></>;
}
export function RiskEventWorkspace({ onExpired }: { onExpired: () => void }) {
  const match = /^\/risk-events(?:\/([1-9][0-9]*))?$/.exec(location.pathname); let initial: RiskEventFilters;
  try { if (!match) throw new Error(); if (match[1]) integer(match[1]); initial = initialFilters(); if (match[1] && location.search) throw new Error(); } catch { return <p className="error" role="alert">风险历史路径或筛选无效。</p>; }
  return <div className="risk-event-workspace">{match[1] ? <EventDetails id={match[1]} onExpired={onExpired} /> : <EventList initial={initial} onExpired={onExpired} />}</div>;
}
