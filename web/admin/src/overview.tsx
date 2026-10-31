import { useCallback, useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import { integer, sources, states, templates } from './strategy-api';
import { getServiceOverview, getStrategyOverview, overviewTime, serviceRoles, validateOverviewFilters, validateServiceFilters } from './overview-api';
import type { LeaseObservation, OverviewFilters, ServiceFilters, ServiceOverview, StrategyObservation, StrategyOverview } from './overview-api';

const roleNames = { api: '本 API', market: '行情服务', strategy: '策略服务', execution: '执行服务', portfolio: '持仓服务', cron: '定期任务' };
const probeNames = { READY: '此次就绪', NOT_READY: '此次未就绪', UNKNOWN: '就绪情况未知', PROBE_NOT_CONFIGURED: '未配置探测', PROBE_FAILED: '探测失败', PROBE_TIMEOUT: '探测超时', PROBE_DENIED: '探测被拒绝', PROBE_CAPACITY: '探测容量不足' };
interface Initial { strategies: OverviewFilters; services: ServiceFilters; strategyAfter: string; serviceAfter: string }
function initial(): Initial {
  const q = new URLSearchParams(location.search);
  for (const [key, value] of q) if (!['account', 'source', 'strategy_limit', 'strategy_after', 'role', 'service_limit', 'service_after'].includes(key) || q.getAll(key).length !== 1 || !value) throw new Error();
  const strategyLimit = q.get('strategy_limit') ?? '20'; const serviceLimit = q.get('service_limit') ?? '4';
  if (!['1', '20', '50'].includes(strategyLimit) || !['1', '4'].includes(serviceLimit)) throw new Error();
  const strategies = { account: q.get('account') ?? '', source: q.get('source') ?? '', limit: Number(strategyLimit) };
  const services = { role: (q.get('role') ?? 'strategy') as ServiceFilters['role'], limit: Number(serviceLimit) };
  validateOverviewFilters(strategies); validateServiceFilters(services); const after = q.get('strategy_after') ?? '0'; integer(after, true);
  const serviceAfter = q.get('service_after') ?? ''; if (new TextEncoder().encode(serviceAfter).length > 128 || serviceAfter.trim() !== serviceAfter || /[\u0000\r\n]/.test(serviceAfter)) throw new Error();
  return { strategies, services, strategyAfter: after, serviceAfter };
}
function useRead<T>(load: (signal: AbortSignal) => Promise<T>, onExpired: () => void, name: string) {
  const [data, setData] = useState<T | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true; let timeout = false; const controller = new AbortController(); setData(null); setError(''); setLoading(true);
    const timer = setTimeout(() => { timeout = true; controller.abort(); }, 6000);
    load(controller.signal).then(value => { if (active) setData(value); }).catch(cause => {
      if (!active) return; const code = cause instanceof ApiError ? cause.code : '';
      if (code === 'UNAUTHENTICATED') { onExpired(); return; }
      setError(timeout ? `${name}读取超时，请重试。` : code === 'FORBIDDEN' ? `当前账户无权查看${name}。` : `暂时无法读取${name}，请稍后重试。`);
    }).finally(() => { clearTimeout(timer); if (active) setLoading(false); });
    return () => { active = false; clearTimeout(timer); controller.abort(); };
  }, [load, name, onExpired, revision]);
  return { data, error, loading, reload: () => setRevision(value => value + 1) };
}
function ReadStatus({ loading, error, reload, name }: { loading: boolean; error: string; reload: () => void; name: string }) {
  return loading ? <p role="status">正在读取{name}…</p> : error ? <div><p className="error" role="alert">{error}</p><button className="button secondary compact" onClick={reload}>重试{name}</button></div> : null;
}
function Fields({ values }: { values: [string, string][] }) { return <dl className="parameter-details">{values.map(([name, value]) => <div key={name}><dt>{name}</dt><dd>{value}</dd></div>)}</dl>; }
function Lease({ value }: { value: LeaseObservation | null }) {
  if (value === null) return <p className="notice">无持久租约槽</p>;
  return <div className="overview-lease"><h4>{!value.owner_recorded ? '未领取的租约槽' : value.expires_at_ms === '0' ? '已释放的保留租约' : '保留租约观测'}</h4><Fields values={[
    ['租约 epoch', value.epoch], ['保留持有者', String(value.owner_recorded)], ['记录期限（UTC）', value.expires_at_ms === '0' ? value.owner_recorded ? '0 · 已清零' : '0 · 未设置' : overviewTime(value.expires_at_ms)],
    ['数据库采样时未到期', String(value.unexpired_at_observation)], ['与存储运行记录匹配', String(value.runtime_matches_lease)],
  ]} /></div>;
}
function StrategyRow({ value }: { value: StrategyObservation }) {
  const s = value.strategy;
  return <article className="overview-instance" aria-label={`策略运行观测 ${s.id}`}><div className="card-heading"><h4>{s.instance_key}</h4><span className={`state-badge state-${s.state.toLowerCase()}`}>存储 · {states[s.state]}</span></div><Fields values={[
    ['策略 ID', s.id], ['模板', templates[s.template]], ['账户 ID', s.account_id], ['交易对 ID', s.instrument_id], ['存储状态', s.state], ['请求目标', s.desired_state], ['修订', s.revision],
    ['请求配置 / 来源', `${s.requested_config_version} / ${sources[s.requested_market_data_source]}`], ['生效配置 / 来源', s.active_config_version === null ? '尚未生效' : `${s.active_config_version} / ${sources[s.active_market_data_source!]}`],
    ['行情模式', s.market_mode], ['存储运行 owner', s.runtime_owner_id || '未记录'], ['存储运行 epoch', s.runtime_owner_epoch],
  ]} /><Lease value={value.lease} /><a className="detail-link" href={`/strategies/${s.id}`} aria-label={`查看概览策略 ${s.id}`}>查看策略 ↗</a></article>;
}
function StrategySummary({ value }: { value: StrategyOverview }) {
  return <><div className="overview-stats" aria-label="策略全范围统计"><div><span>筛选范围内策略</span><strong data-overview-total>{value.total}</strong></div><div><span>请求运行的策略</span><strong data-overview-requested>{value.requested_running}</strong></div></div><p className="muted">数据库采样（UTC）：<time>{overviewTime(value.observed_at_ms)}</time></p><div className="overview-state-counts">{value.stored_states.map(entry => <span className="badge" key={entry.state}>{states[entry.state]}（存储） · {entry.count}</span>)}</div></>;
}
function ServiceSummary({ value }: { value: ServiceOverview }) {
  return <><div className="overview-stats"><div><span>{value.membership_source === 'LOCAL_API' ? '当前 API 实例' : '符合条件的注册节点'}</span><strong data-service-total>{value.total}</strong></div><div><span>探测配置</span><strong className="overview-stat-label">{value.probe_configured ? '已配置' : '未配置'}</strong></div></div><p className="muted">成员采样（UTC）：<time>{overviewTime(value.membership_observed_at_ms)}</time></p><p className="notice">{value.membership_source === 'LOCAL_API' ? '仅表示处理此请求的 API，不枚举其他 API 副本。' : '仅包含当前发现范围内健康、启用的 gRPC 注册，不是全部部署进程数。'}</p></>;
}
function Content({ initialValue, onExpired }: { initialValue: Initial; onExpired: () => void }) {
  const [filters, setFilters] = useState(initialValue.strategies); const [draft, setDraft] = useState(filters); const [formError, setFormError] = useState('');
  const [services, setServices] = useState(initialValue.services); const [serviceDraft, setServiceDraft] = useState(services);
  const [strategyCursors, setStrategyCursors] = useState([initialValue.strategyAfter]); const [serviceCursors, setServiceCursors] = useState([initialValue.serviceAfter]);
  const strategyAfter = strategyCursors[strategyCursors.length - 1]; const serviceAfter = serviceCursors[serviceCursors.length - 1];
  const loadStrategies = useCallback((signal: AbortSignal) => getStrategyOverview(strategyAfter, filters, signal), [strategyAfter, filters]);
  const loadServices = useCallback((signal: AbortSignal) => getServiceOverview(serviceAfter, services, signal), [serviceAfter, services]);
  const strategyQuery = useRead(loadStrategies, onExpired, '策略概览'); const serviceQuery = useRead(loadServices, onExpired, '服务观测');
  useEffect(() => {
    const q = new URLSearchParams(); if (filters.account) q.set('account', filters.account); if (filters.source) q.set('source', filters.source); if (filters.limit !== 20) q.set('strategy_limit', String(filters.limit)); if (strategyAfter !== '0') q.set('strategy_after', strategyAfter);
    if (services.role !== 'strategy') q.set('role', services.role); if (services.limit !== 4) q.set('service_limit', String(services.limit)); if (serviceAfter) q.set('service_after', serviceAfter);
    history.replaceState(null, '', `/overview${q.size ? `?${q}` : ''}`);
  }, [filters, strategyAfter, services, serviceAfter]);
  function applyStrategies(event: FormEvent) {
    event.preventDefault(); try { validateOverviewFilters(draft); } catch { setFormError('请输入规范账户 ID，或留空查询全部。'); return; }
    setFormError(''); setFilters({ ...draft }); setStrategyCursors(['0']);
  }
  function applyServices(event: FormEvent) { event.preventDefault(); validateServiceFilters(serviceDraft); setServices({ ...serviceDraft }); setServiceCursors(['']); }
  return <><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">RUNTIME OBSERVATIONS</span><h2>运行概览</h2><p>分开观察服务就绪、策略存储状态与运行目标。</p></div></div>
    <p className="notice overview-notice">以下均为各次采样。注册在线、存储 RUNNING 或匹配租约都不是当前交易许可；读取不会启动策略、续租或恢复风险。</p>
    <section className="card overview-card" aria-label="服务运行观测"><div className="card-heading"><h3>服务运行观测</h3><button className="button secondary compact" disabled={serviceQuery.loading} onClick={serviceQuery.reload}>刷新服务观测</button></div>
      <form className="overview-service-filters" onSubmit={applyServices}><label>服务角色<select aria-label="服务角色" value={serviceDraft.role} onChange={event => setServiceDraft({ ...serviceDraft, role: event.target.value as ServiceFilters['role'] })}>{serviceRoles.map(role => <option key={role} value={role}>{roleNames[role]} · {role}</option>)}</select></label><label>每页节点<select aria-label="每页节点" value={serviceDraft.limit} onChange={event => setServiceDraft({ ...serviceDraft, limit: Number(event.target.value) })}>{[1, 4].map(limit => <option key={limit}>{limit}</option>)}</select></label><button className="button secondary" type="submit">查询服务角色</button></form>
      <ReadStatus {...serviceQuery} name="服务观测" />{serviceQuery.data && <><ServiceSummary value={serviceQuery.data} />{serviceQuery.data.items.length === 0 ? <p className="empty-state">当前页没有符合条件的服务节点</p> : <div className="overview-node-grid">{serviceQuery.data.items.map(item => <article className="overview-node" key={item.instance_id} aria-label={`服务节点 ${item.instance_id}`}><div className="card-heading"><h4>{item.instance_id}</h4><span className={`badge probe-${item.status.toLowerCase()}`}>{probeNames[item.status]}</span></div><Fields values={[
        ['节点 ID', item.instance_id], ['声明版本', item.declared_version], ['探测结果', item.status], ['探测时间（UTC）', item.probed_at_ms === null ? '未探测' : overviewTime(item.probed_at_ms)],
      ]} /></article>)}</div>}<nav className="pagination" aria-label="服务节点分页"><button className="button secondary" disabled={serviceQuery.loading || serviceCursors.length === 1} onClick={() => setServiceCursors(serviceCursors.slice(0, -1))}>上一页节点</button><span>当前 {serviceQuery.data.items.length} 个节点</span><button className="button secondary" disabled={serviceQuery.loading || !serviceQuery.data.next_after_instance_id} onClick={() => setServiceCursors([...serviceCursors, serviceQuery.data!.next_after_instance_id])}>下一页节点</button></nav><p className="notice">版本为配置或注册声明。成员与探测分别采样，就绪结果不证明该实例持有策略执行权。</p></>}
    </section>
    <section className="card overview-card" aria-label="策略运行观测"><div className="card-heading"><h3>策略运行观测</h3><button className="button secondary compact" disabled={strategyQuery.loading} onClick={strategyQuery.reload}>刷新策略概览</button></div>
      <form className="overview-strategy-filters" onSubmit={applyStrategies}><label>概览账户 ID<input aria-label="概览账户 ID" maxLength={64} value={draft.account} onChange={event => setDraft({ ...draft, account: event.target.value })} placeholder="留空查询全部" /></label><label>请求行情来源<select aria-label="请求行情来源" value={draft.source} onChange={event => setDraft({ ...draft, source: event.target.value })}><option value="">全部来源</option>{Object.entries(sources).map(([source, name]) => <option key={source} value={source}>{name}</option>)}</select></label><label>每页策略<select aria-label="每页策略" value={draft.limit} onChange={event => setDraft({ ...draft, limit: Number(event.target.value) })}>{[1, 20, 50].map(limit => <option key={limit}>{limit}</option>)}</select></label><button className="button secondary" type="submit">筛选策略概览</button></form>
      {formError && <p className="error" role="alert">{formError}</p>}<ReadStatus {...strategyQuery} name="策略概览" />{strategyQuery.data && <><StrategySummary value={strategyQuery.data} />{strategyQuery.data.items.length === 0 ? <p className="empty-state">当前页没有匹配的策略</p> : <div className="overview-strategy-grid">{strategyQuery.data.items.map(value => <StrategyRow key={value.strategy.id} value={value} />)}</div>}<nav className="pagination" aria-label="概览策略分页"><button className="button secondary" disabled={strategyQuery.loading || strategyCursors.length === 1} onClick={() => setStrategyCursors(strategyCursors.slice(0, -1))}>上一页策略观测</button><span>当前 {strategyQuery.data.items.length} 个策略</span><button className="button secondary" disabled={strategyQuery.loading || strategyQuery.data.next_after_id === '0'} onClick={() => setStrategyCursors([...strategyCursors, strategyQuery.data!.next_after_id])}>下一页策略观测</button></nav><p className="notice">统计覆盖全部筛选范围，与当前页不同。请求目标、存储状态及租约事实分别记录，均不授予新增风险。</p></>}
    </section></>;
}
export function OverviewWorkspace({ onExpired }: { onExpired: () => void }) {
  let value: Initial;
  try { if (location.pathname !== '/overview') throw new Error(); value = initial(); } catch { return <p className="error" role="alert">概览路径或筛选无效。</p>; }
  return <div className="overview-workspace"><Content initialValue={value} onExpired={onExpired} /></div>;
}
