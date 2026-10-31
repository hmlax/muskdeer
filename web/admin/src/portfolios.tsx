import { useCallback, useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import { integer } from './strategy-api';
import { getPortfolio, listPortfolios, portfolioAccount } from './portfolio-api';
import type { Portfolio, PortfolioBalance, PortfolioFilters } from './portfolio-api';

function usePortfolioRead<T>(load: (signal: AbortSignal) => Promise<T>, onExpired: () => void) {
  const [data, setData] = useState<T | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [revision, setRevision] = useState(0);
  useEffect(() => {
    setData(null); setError(''); setLoading(true);
    let active = true; let timeout = false; const controller = new AbortController();
    const timer = setTimeout(() => { timeout = true; controller.abort(); }, 6000);
    load(controller.signal).then(value => { if (active) setData(value); }).catch(cause => {
      if (!active) return;
      const code = cause instanceof ApiError ? cause.code : '';
      if (code === 'UNAUTHENTICATED') { onExpired(); return; }
      setError(timeout ? '持仓读取超时，请重试。' : code === 'NOT_FOUND' ? '未找到此账户、交易对或持仓。' : code === 'FORBIDDEN' ? '当前账户无权查看持仓。' : '暂时无法读取持仓，请稍后重试。');
    }).finally(() => { clearTimeout(timer); if (active) setLoading(false); });
    return () => { active = false; clearTimeout(timer); controller.abort(); };
  }, [load, onExpired, revision]);
  return { data, error, loading, reload: () => setRevision(value => value + 1) };
}
function QueryStatus({ loading, error, retry }: { loading: boolean; error: string; retry: () => void }) {
  return loading ? <p role="status">正在读取持仓…</p> : error ? <div><p className="error" role="alert">{error}</p><button className="button secondary" onClick={retry}>重试持仓读取</button></div> : null;
}
function utc(value: string): string {
  const milliseconds = Number(value); const date = new Date(milliseconds);
  return Number.isSafeInteger(milliseconds) && !Number.isNaN(date.getTime()) ? date.toISOString() : value;
}
function Fields({ values }: { values: [string, string][] }) { return <dl className="parameter-details">{values.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>; }
function CoreBalance({ value, asset }: { value: PortfolioBalance | null; asset: string }) {
  return <section className="card portfolio-card" aria-label={`${asset} 核心余额`}><div className="card-heading"><h3>{asset} 核心余额</h3><span className="badge">独立权威记录</span></div>{value ? <Fields values={[
    ['余额记录 ID', value.id], [`可用余额（${asset}）`, value.available], [`冻结余额（${asset}）`, value.frozen], ['核心余额修订', value.revision], ['核心记录时间（UTC）', utc(value.updated_at_ms)],
  ]} /> : <p className="notice">尚无 {asset} 核心余额记录；不能视为零资金。</p>}</section>;
}
function PortfolioDetail({ value }: { value: Portfolio }) {
  const p = value.projection; const base = value.base_asset; const quote = value.quote_asset;
  return <><section className="card portfolio-card" aria-label="账户持仓详情"><div className="card-heading"><h3>账户持仓 · {value.account_id}</h3><span className="badge">账户级投影</span></div><Fields values={[
    ['持仓记录 ID', value.id], ['账户', value.account_id], ['交易对', value.symbol], ['交易对 ID', value.instrument_id], ['执行场所', '模拟核心'], ['账户配置', value.account_enabled ? '启用' : '停用'], ['账户修订', value.account_revision], ['投影修订', p.revision], ['投影创建时间（UTC）', utc(value.created_at_ms)], ['投影更新时间（UTC）', utc(value.updated_at_ms)],
    [`期初数量（${base}）`, p.opening_quantity], [`期初现金（${quote}）`, p.opening_cash_quote], [`期初成本（${quote}）`, p.opening_cost_quote], [`持仓数量（${base}）`, p.quantity], [`现金投影（${quote}）`, p.cash_quote], [`成本（${quote}）`, p.cost_quote], [`已实现损益（${quote}）`, p.realized_pnl_quote], [`费用累计（${quote}）`, p.fees_quote],
  ]} /><p className="notice">这是共享账户内所有策略的投影，不是某个策略的独占持仓。仅展示存储的已实现损益，不作行情估值。</p></section>
    <section className="card portfolio-card" aria-label="投影诊断与成交进度"><div className="card-heading"><h3>投影诊断与成交进度</h3><span className="badge">不授予新增风险</span></div><Fields values={[
      ['上次投影诊断', p.stored_reconciled ? '曾通过对账' : '未通过对账'], ['存储诊断代码', p.error_code || '—'], ['投影最后处理源 ID', p.last_source_sequence], ['当前核心成交数', value.source.count], ['当前核心最大源 ID', value.source.latest_source_sequence], ['当前收录事实数', value.collected.count], ['当前收录最大源 ID', value.collected.latest_source_sequence],
    ]} /><p className="notice">存储诊断可能早于当前核心事实；计数相同也不能证明已对账。读取不补账、不释放预留，也不开放风险。</p></section>
    <div className="portfolio-balances"><CoreBalance value={value.core_base_balance} asset={base} /><CoreBalance value={value.core_quote_balance} asset={quote} /></div></>;
}
function PortfolioDetails({ id, onExpired }: { id: string; onExpired: () => void }) {
  const load = useCallback((signal: AbortSignal) => getPortfolio(id, signal), [id]); const query = usePortfolioRead(load, onExpired);
  return <><a className="back-link" href={query.data ? `/portfolios?account_id=${encodeURIComponent(query.data.account_id)}` : '/portfolios'}>← 账户持仓</a><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">ACCOUNT PORTFOLIO</span><h2>账户持仓详情</h2><p>存储投影与核心余额分别展示。</p></div><button className="button secondary compact" disabled={query.loading} onClick={query.reload}>刷新持仓</button></div><QueryStatus loading={query.loading} error={query.error} retry={query.reload} />{query.data && <PortfolioDetail value={query.data} />}</>;
}
function initialFilters(): PortfolioFilters {
  const values = new URLSearchParams(location.search);
  for (const key of values.keys()) if (!['account_id', 'instrument_id'].includes(key) || values.getAll(key).length !== 1) throw new Error();
  const account = values.get('account_id') ?? ''; const instrument = values.get('instrument_id') ?? '';
  if (values.has('account_id')) portfolioAccount(account); if (values.has('instrument_id')) integer(instrument);
  return { account, instrument, limit: 20 };
}
function PortfolioList({ initial, onExpired }: { initial: PortfolioFilters; onExpired: () => void }) {
  const [draft, setDraft] = useState(initial); const [filters, setFilters] = useState(initial); const [formError, setFormError] = useState(''); const [cursors, setCursors] = useState(['0']); const cursor = cursors[cursors.length - 1];
  const load = useCallback((signal: AbortSignal) => listPortfolios(cursor, filters, signal), [cursor, filters]); const query = usePortfolioRead(load, onExpired);
  function apply(event: FormEvent) {
    event.preventDefault();
    try { if (draft.account) portfolioAccount(draft.account); if (draft.instrument) integer(draft.instrument); } catch { setFormError('请输入规范账户 ID 和正整数交易对 ID，或留空查询全部。'); return; }
    setFormError(''); setCursors(['0']); setFilters({ ...draft });
    const values = new URLSearchParams(); if (draft.account) values.set('account_id', draft.account); if (draft.instrument) values.set('instrument_id', draft.instrument);
    history.replaceState(null, '', `/portfolios${values.size ? `?${values}` : ''}`);
  }
  return <><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">ACCOUNT POSITIONS</span><h2>账户持仓</h2><p>账户投影、成交收录与核心资金各有来源。</p></div><button className="button secondary compact" disabled={query.loading} onClick={query.reload}>刷新持仓</button></div><section className="card portfolio-card" aria-label="账户持仓查询"><p className="notice">共享账户只展示一份持仓。上次生产者诊断不是当前风险许可，缺失核心余额不等于零资金。</p>
    <form className="portfolio-filters" onSubmit={apply}><label>账户 ID<input aria-label="账户 ID" maxLength={64} value={draft.account} onChange={event => setDraft({ ...draft, account: event.target.value })} placeholder="留空查询全部" /></label><label>交易对 ID<input aria-label="交易对 ID" inputMode="numeric" maxLength={19} value={draft.instrument} onChange={event => setDraft({ ...draft, instrument: event.target.value })} placeholder="留空查询全部" /></label><label>每页持仓<select aria-label="每页持仓" value={draft.limit} onChange={event => setDraft({ ...draft, limit: Number(event.target.value) })}>{[1, 20, 50].map(limit => <option key={limit}>{limit}</option>)}</select></label><button className="button secondary" type="submit">筛选持仓</button></form>
    {formError && <p className="error" role="alert">{formError}</p>}<QueryStatus loading={query.loading} error={query.error} retry={query.reload} />{query.data && <>{query.data.portfolios.length === 0 ? <p className="empty-state">没有匹配的账户持仓</p> : <div className="table-scroll" role="region" aria-label="账户持仓列表" tabIndex={0}><table><thead><tr><th>账户 / 交易对</th><th>存储数量 / 现金</th><th>已实现损益 / 费用</th><th>上次诊断 / 收录</th><th>详情</th></tr></thead><tbody>{query.data.portfolios.map(value => <tr key={value.id}><td>{value.account_id}<small>{value.symbol} · {value.account_enabled ? '账户启用' : '账户停用'}</small></td><td>{value.projection.quantity} {value.base_asset}<small>{value.projection.cash_quote} {value.quote_asset}</small></td><td>{value.projection.realized_pnl_quote} {value.quote_asset}<small>{value.projection.fees_quote} {value.quote_asset}</small></td><td>{value.projection.stored_reconciled ? '曾通过对账' : '未通过对账'}<small>核心 {value.source.count} · 已收录 {value.collected.count}</small></td><td><a className="detail-link" href={`/portfolios/${value.id}`} aria-label={`查看持仓 ${value.id}`}>查看 ↗</a></td></tr>)}</tbody></table></div>}
    <nav className="pagination" aria-label="持仓分页"><button className="button secondary" disabled={query.loading || cursors.length === 1} onClick={() => setCursors(cursors.slice(0, -1))}>上一页持仓</button><span>第 {cursors.length} 页</span><button className="button secondary" disabled={query.loading || query.data.next_after_id === '0'} onClick={() => setCursors([...cursors, query.data!.next_after_id])}>下一页持仓</button></nav></>}</section></>;
}
export function PortfolioWorkspace({ onExpired }: { onExpired: () => void }) {
  const match = /^\/portfolios(?:\/([1-9][0-9]*))?$/.exec(location.pathname);
  let initial: PortfolioFilters;
  try { if (!match) throw new Error(); if (match[1]) integer(match[1]); initial = initialFilters(); if (match[1] && location.search) throw new Error(); } catch { return <p className="error" role="alert">持仓路径或筛选无效。</p>; }
  return <div className="portfolio-workspace">{match[1] ? <PortfolioDetails id={match[1]} onExpired={onExpired} /> : <PortfolioList initial={initial} onExpired={onExpired} />}</div>;
}
