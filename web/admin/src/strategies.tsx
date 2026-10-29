import { useCallback, useEffect, useState } from 'react';
import { ApiError } from './api';
import type { Session } from './api';
import { StrategyControls } from './operations';
import { CreationForm } from './creation';
import { PublicationForm } from './publication';
import { RiskPanel } from './risk';
import { OrderPanel } from './orders';
import { getStrategy, listStrategies, listVersions, sources, states, templates } from './strategy-api';
import type { ConfigVersion, Parameters, Strategy, StrategyDetail } from './strategy-api';

function useQuery<T>(load: (signal: AbortSignal) => Promise<T>, onExpired: () => void) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    let timedOut = false;
    const controller = new AbortController();
    const timer = setTimeout(() => { timedOut = true; controller.abort(); }, 6000);
    setLoading(true); setError(''); setData(null);
    load(controller.signal).then(value => { if (active) setData(value); }).catch(cause => {
      if (!active) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      if (timedOut) { setError('请求超时，请重试。'); return; }
      const code = cause instanceof ApiError ? cause.code : '';
      setError(code === 'NOT_FOUND' ? '策略不存在或已删除。' : code === 'FORBIDDEN' ? '当前账户无权查看该资源。' : '暂时无法读取策略数据，请稍后重试。');
    }).finally(() => { clearTimeout(timer); if (active) setLoading(false); });
    return () => { active = false; clearTimeout(timer); controller.abort(); };
  }, [load, onExpired, revision]);
  return { data, error, loading, reload: () => setRevision(value => value + 1) };
}

function QueryState({ loading, error, retry }: { loading: boolean; error: string; retry: () => void }) {
  if (loading) return <div className="card loading" role="status"><span className="spinner" />正在读取策略数据…</div>;
  if (error) return <div className="card"><div className="error" role="alert">{error}</div><button className="button secondary" onClick={retry}>重试</button></div>;
  return null;
}

function Pages({ cursors, next, busy, setCursors }: { cursors: string[]; next: string; busy: boolean; setCursors: (value: string[]) => void }) {
  return <nav className="pagination" aria-label="分页">
    <button className="button secondary" disabled={busy || cursors.length === 1} onClick={() => setCursors(cursors.slice(0, -1))}>上一页</button>
    <span>第 {cursors.length} 页</span>
    <button className="button secondary" disabled={busy || next === '0'} onClick={() => setCursors([...cursors, next])}>下一页</button>
  </nav>;
}

function SourcePair({ strategy }: { strategy: Strategy }) {
  return <div className="source-pair"><span>请求 · {sources[strategy.requested_market_data_source]}</span><small>生效 · {strategy.active_market_data_source ? sources[strategy.active_market_data_source] : '未生效'}</small></div>;
}

export function StrategyWorkspace({ onExpired, session }: { onExpired: () => void; session: Session }) {
  const id = /^\/strategies\/([1-9][0-9]{0,18})$/.exec(location.pathname)?.[1];
  return id ? <StrategyDetails id={id} onExpired={onExpired} session={session} /> : <StrategyList onExpired={onExpired} session={session} />;
}

function StrategyList({ onExpired, session }: { onExpired: () => void; session: Session }) {
  const [creating, setCreating] = useState(false);
  const [cursors, setCursors] = useState(['0']);
  const cursor = cursors[cursors.length - 1];
  const load = useCallback((signal: AbortSignal) => listStrategies(cursor, signal), [cursor]);
  const query = useQuery(load, onExpired);
  return <div className="strategy-workspace">
    {session.strategy_controls_enabled && session.role !== 'viewer' && !creating && <button className="button primary creation-open" onClick={() => setCreating(true)}>创建策略</button>}
    {creating && <CreationForm onExpired={onExpired} onCreated={query.reload} onClose={() => setCreating(false)} />}
    <div className="workspace-heading"><div className="page-heading"><span className="eyebrow">STRATEGIES</span><h2>策略实例</h2><p>查看运行状态、行情来源和配置版本。</p></div><button className="button secondary compact" onClick={query.reload} disabled={query.loading}>刷新</button></div>
    <QueryState loading={query.loading} error={query.error} retry={query.reload} />
    {query.data && <><div className="card data-card"><div className="card-heading"><h3>实例列表</h3><span className="badge">只读视图</span></div>
      {query.data.strategies.length === 0 ? <div className="empty-state"><h3>暂无策略实例</h3><p>当前没有可查看的策略。</p></div> : <div className="table-scroll" role="region" aria-label="策略列表" tabIndex={0}><table><thead><tr><th>策略 / 账户</th><th>模板</th><th>行情来源</th><th>实际状态 / 请求目标</th><th>配置</th><th><span className="sr-only">详情</span></th></tr></thead><tbody>
        {query.data.strategies.map(strategy => <tr key={strategy.id}>
          <td><a className="instance-link" href={`/strategies/${strategy.id}`}>{strategy.instance_key}</a><small>{strategy.account_id}</small></td>
          <td>{templates[strategy.template]}<small>{strategy.market_mode === 'REPLAY' ? '回放行情' : '实时行情'}</small></td>
          <td><SourcePair strategy={strategy} /></td>
          <td><span className={`state-badge ${strategy.state === 'RUNNING' ? 'running' : ''}`}>{states[strategy.state]}</span><small>目标 · {states[strategy.desired_state]}</small></td>
          <td>请求 v{strategy.requested_config_version}<small>生效 {strategy.active_config_version ? `v${strategy.active_config_version}` : '—'}</small></td>
          <td><a className="detail-link" href={`/strategies/${strategy.id}`} aria-label={`查看 ${strategy.instance_key}`}>查看 ↗</a></td>
        </tr>)}
      </tbody></table></div>}
    </div><Pages cursors={cursors} next={query.data.next_after_id} busy={query.loading} setCursors={setCursors} /></>}
  </div>;
}

function parameterRows(parameters: Parameters): [string, string][] {
  if (parameters.kind === 'grid') {
    const p = parameters.values;
    return [['下界价格', p.lower_price], ['上界价格', p.upper_price], ['网格数量', String(p.grid_count)], ['总预算（报价资产）', p.total_budget], ['单笔名义额（报价资产）', p.per_order_notional], ['库存上限（基础资产）', p.inventory_limit], ['行情最大年龄（ms）', p.max_market_age_ms]];
  }
  const p = parameters.values;
  return [['报价档数', String(p.quote_levels)], ['档距（BPS）', p.level_spacing_bps], ['基础价差（BPS）', p.base_spread_bps], ['单档名义额（报价资产）', p.level_notional], ['单侧预算（报价资产）', p.side_budget], ['目标库存（基础资产）', p.inventory_target], ['库存软限（基础资产）', p.inventory_soft_limit], ['库存硬限（基础资产）', p.inventory_hard_limit], ['改价阈值（BPS）', p.reprice_bps], ['最小刷新间隔（ms）', p.min_refresh_interval_ms], ['报价最大年龄（ms）', p.max_quote_age_ms], ['行情最大年龄（ms）', p.max_market_age_ms], ['对冲', p.hedge_enabled ? '已启用' : '未启用'], ['对冲阈值（基础资产）', p.hedge_threshold], ['对冲最长等待（ms）', p.hedge_max_wait_ms], ['最大对冲积压（基础资产）', p.max_hedge_backlog]];
}

function ConfigCard({ title, config }: { title: string; config: ConfigVersion | null }) {
  return <section className="card config-card" aria-label={title}><div className="card-heading"><h3>{title}</h3><span className="badge">{config ? `v${config.version}` : '未生效'}</span></div>
    {!config ? <p className="notice">尚无已生效配置。</p> : <><dl className="parameter-details"><div><dt>行情来源</dt><dd>{sources[config.market_data_source]}</dd></div><div><dt>代码版本</dt><dd>{config.code_version}</dd></div><div><dt>提交人</dt><dd>{config.created_by}</dd></div>
      {config.parameters && parameterRows(config.parameters).map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}
    </dl>{!config.parameters && <p className="notice">此历史版本仅含元数据，没有可运行参数。</p>}</>}
  </section>;
}

function StrategyDetails({ id, onExpired, session }: { id: string; onExpired: () => void; session: Session }) {
  const [publication, setPublication] = useState<StrategyDetail | null>(null);
  const [cursors, setCursors] = useState(['0']);
  const [selected, setSelected] = useState<ConfigVersion | null>(null);
  const cursor = cursors[cursors.length - 1];
  const load = useCallback(async (signal: AbortSignal) => {
    const [detail, history] = await Promise.all([getStrategy(id, signal), listVersions(id, cursor, signal)]);
    return { detail, history };
  }, [id, cursor]);
  const query = useQuery(load, onExpired);
  const strategy = query.data?.detail.strategy;
  return <div className="strategy-workspace">
    <a className="back-link" href="/strategies">← 策略实例</a>
    <div className="workspace-heading"><div className="page-heading"><span className="eyebrow">STRATEGY DETAIL</span><h2>{strategy?.instance_key ?? '策略详情'}</h2><p>请求目标与实际生效状态分别展示。</p></div><button className="button secondary compact" onClick={() => { setSelected(null); query.reload(); }} disabled={query.loading}>刷新</button></div>
    <QueryState loading={query.loading} error={query.error} retry={query.reload} />
    <StrategyControls strategy={strategy} loading={query.loading || query.error !== ''} session={session} onExpired={onExpired} onRefresh={query.reload} />
    <RiskPanel id={id} current={strategy} loading={query.loading || query.error !== ''} session={session} onExpired={onExpired} onRefresh={query.reload} />
    <OrderPanel id={id} available={strategy !== undefined} onExpired={onExpired} />
    {session.strategy_controls_enabled && session.role !== 'viewer' && !publication && query.data?.detail.requested_config.parameters && !(query.data.detail.requested_config.parameters.kind === 'market_maker' && query.data.detail.requested_config.parameters.values.hedge_enabled) && <button className="button primary" onClick={() => setPublication(query.data!.detail)}>发布参数与换源</button>}
    {publication && <PublicationForm initial={publication} current={query.data?.detail ?? null} loading={query.loading || query.error !== ''} onExpired={onExpired} onRefresh={query.reload} onClose={() => setPublication(null)} />}
    {query.data && strategy && <><div className="card identity-card"><dl className="parameter-details"><div><dt>实例 ID</dt><dd>{strategy.id}</dd></div><div><dt>模板</dt><dd>{templates[strategy.template]}</dd></div><div><dt>账户</dt><dd>{strategy.account_id}</dd></div><div><dt>交易对 ID</dt><dd>{strategy.instrument_id}</dd></div><div><dt>行情模式</dt><dd>{strategy.market_mode === 'REPLAY' ? '回放' : '实时'}</dd></div><div><dt>实际状态</dt><dd>{states[strategy.state]}</dd></div><div><dt>请求目标</dt><dd>{states[strategy.desired_state]}</dd></div><div><dt>实例修订</dt><dd>{strategy.revision}</dd></div></dl></div>
      <div className="config-columns"><ConfigCard title="请求配置" config={query.data.detail.requested_config} /><ConfigCard title="生效配置" config={query.data.detail.active_config} /></div>
      <div className="card data-card"><div className="card-heading"><h3>配置版本历史</h3><span className="badge">只读</span></div><div className="table-scroll" role="region" aria-label="配置版本" tabIndex={0}><table><thead><tr><th>版本</th><th>行情来源</th><th>代码版本</th><th>提交人</th><th>参数</th></tr></thead><tbody>
        {query.data.history.versions.map(version => <tr key={version.id}><td>v{version.version}</td><td>{sources[version.market_data_source]}</td><td>{version.code_version}</td><td>{version.created_by}</td><td><button className="detail-link link-button" onClick={() => setSelected(version)} aria-label={`查看版本 ${version.version}`}>查看参数 ↗</button></td></tr>)}
      </tbody></table></div></div>
      <Pages cursors={cursors} next={query.data.history.next_after_id} busy={query.loading} setCursors={value => { setSelected(null); setCursors(value); }} />
      {selected && <ConfigCard title="历史版本参数" config={selected} />}
    </>}
  </div>;
}
