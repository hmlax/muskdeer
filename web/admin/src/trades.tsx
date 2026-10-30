import { useCallback, useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import { integer, sources } from './strategy-api';
import { getTrade, listTrades } from './trade-api';
import type { Trade, TradeFilters } from './trade-api';

function useTradeRead<T>(load: (signal: AbortSignal) => Promise<T>, onExpired: () => void) {
  const [data, setData] = useState<T | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [revision, setRevision] = useState(0);
  useEffect(() => {
    setData(null); setError(''); setLoading(true);
    let active = true; let timeout = false; const controller = new AbortController();
    const timer = setTimeout(() => { timeout = true; controller.abort(); }, 6000);
    load(controller.signal).then(value => { if (active) setData(value); }).catch(cause => {
      if (!active) return;
      const code = cause instanceof ApiError ? cause.code : '';
      if (code === 'UNAUTHENTICATED') { onExpired(); return; }
      setError(timeout ? '成交读取超时，请重试。' : code === 'NOT_FOUND' ? '未找到此成交、订单或策略。' : code === 'FORBIDDEN' ? '当前账户无权查看成交。' : '暂时无法读取成交，请稍后重试。');
    }).finally(() => { clearTimeout(timer); if (active) setLoading(false); });
    return () => { active = false; clearTimeout(timer); controller.abort(); };
  }, [load, onExpired, revision]);
  return { data, error, loading, reload: () => setRevision(value => value + 1) };
}
function TradeStatus({ loading, error, retry }: { loading: boolean; error: string; retry: () => void }) {
  return loading ? <p role="status">正在读取成交…</p> : error ? <div><p className="error" role="alert">{error}</p><button className="button secondary" onClick={retry}>重试成交读取</button></div> : null;
}
function timestamp(value: string): string {
  const milliseconds = Number(value); const date = new Date(milliseconds);
  return Number.isSafeInteger(milliseconds) && !Number.isNaN(date.getTime()) ? date.toISOString() : value;
}
function TradeDetail({ trade }: { trade: Trade }) {
  const fields: [string, string][] = [
    ['核心成交 ID', trade.id], ['交易流水 ID', trade.trade_id], ['执行场所', '模拟核心'], ['策略 ID', trade.strategy_id], ['账户', trade.account_id], ['交易对 ID', trade.instrument_id],
    ['历史配置与来源', `v${trade.config_version} · ${sources[trade.market_data_source]}`], ['核心订单 ID', trade.core_order_id], ['客户端订单 ID', trade.client_order_id],
    ['原 OMS 订单 ID', trade.order_id ?? '原始提交未关联'], ['方向', trade.side], ['价格', trade.price], ['成交数量', trade.quantity], ['名义额', trade.quote_notional],
    ['手续费', trade.fee_amount], ['费用资产', trade.fee_asset], ['核心记录时间（UTC）', timestamp(trade.recorded_at_ms)],
    ['持仓事实收录', trade.portfolio_fact ? '已收录成交事实' : '尚未收录'],
    ['收录记录 ID', trade.portfolio_fact?.id ?? '—'], ['收录时间（UTC）', trade.portfolio_fact ? timestamp(trade.portfolio_fact.recorded_at_ms) : '—'],
  ];
  return <section className="card trade-detail" aria-label="成交详情"><div className="card-heading"><h3>成交详情 · {trade.id}</h3><span className="badge">权威成交</span></div><dl className="parameter-details">{fields.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl><p className="notice">已收录成交事实不表示持仓完整或已对账。记录时间是数据库落库时间，不是交易所执行时间。</p></section>;
}
function TradeDetails({ strategy, id, onExpired }: { strategy: string; id: string; onExpired: () => void }) {
  const load = useCallback((signal: AbortSignal) => getTrade(strategy, id, signal), [strategy, id]);
  const query = useTradeRead(load, onExpired);
  return <><a className="back-link" href={`/strategies/${strategy}/trades`}>← 策略成交</a><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">TRADE DETAIL</span><h2>核心成交详情</h2><p>成交事实与持仓侧收录分别展示。</p></div><button className="button secondary compact" disabled={query.loading} onClick={query.reload}>刷新成交</button></div><TradeStatus loading={query.loading} error={query.error} retry={query.reload} />{query.data && <TradeDetail trade={query.data} />}</>;
}
function TradeList({ strategy, onExpired }: { strategy: string; onExpired: () => void }) {
  const initial: TradeFilters = { orderID: '', side: '', limit: 20 };
  const [draft, setDraft] = useState(initial); const [filters, setFilters] = useState(initial); const [formError, setFormError] = useState(''); const [cursors, setCursors] = useState(['0']);
  const cursor = cursors[cursors.length - 1];
  const load = useCallback((signal: AbortSignal) => listTrades(strategy, cursor, filters, signal), [strategy, cursor, filters]);
  const query = useTradeRead(load, onExpired);
  function apply(event: FormEvent) {
    event.preventDefault();
    try { if (draft.orderID) integer(draft.orderID); } catch { setFormError('订单 ID 需为规范正整数，或留空查询全部。'); return; }
    setFormError(''); setCursors(['0']); setFilters({ ...draft });
  }
  return <><a className="back-link" href={`/strategies/${strategy}`}>← 策略详情</a><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">AUTHORITATIVE TRADES</span><h2>策略成交</h2><p>策略 {strategy} · 模拟核心真实成交</p></div><button className="button secondary compact" disabled={query.loading} onClick={query.reload}>刷新成交</button></div>
    <section className="card trade-panel" aria-label="策略成交查询"><p className="notice">以核心成交为准；持仓侧尚未收录不影响成交事实。读取不会投递回报、补账或释放订单预留。</p>
      <form className="trade-filters" onSubmit={apply}><label>原 OMS 订单 ID<input aria-label="原 OMS 订单 ID" inputMode="numeric" maxLength={19} value={draft.orderID} onChange={event => setDraft({ ...draft, orderID: event.target.value })} placeholder="留空查询全部" /></label><label>成交方向<select aria-label="成交方向" value={draft.side} onChange={event => setDraft({ ...draft, side: event.target.value })}><option value="">全部方向</option><option>BUY</option><option>SELL</option></select></label><label>每页成交<select aria-label="每页成交" value={draft.limit} onChange={event => setDraft({ ...draft, limit: Number(event.target.value) })}>{[1, 20, 50].map(limit => <option key={limit}>{limit}</option>)}</select></label><button className="button secondary" type="submit">筛选成交</button></form>
      {formError && <p role="alert" className="error">{formError}</p>}<TradeStatus loading={query.loading} error={query.error} retry={query.reload} />
      {query.data && <>{query.data.trades.length === 0 ? <p className="empty-state">没有匹配的成交</p> : <div className="table-scroll" role="region" aria-label="成交列表" tabIndex={0}><table><thead><tr><th>核心成交 / 历史来源</th><th>方向 / 价格 / 数量</th><th>名义额 / 手续费</th><th>持仓事实收录</th><th>详情</th></tr></thead><tbody>{query.data.trades.map(trade => <tr key={trade.id}><td>{trade.id}<small>v{trade.config_version} · {sources[trade.market_data_source]}</small></td><td>{trade.side}<small>{trade.price} × {trade.quantity}</small></td><td>{trade.quote_notional}<small>{trade.fee_amount} {trade.fee_asset}</small></td><td>{trade.portfolio_fact ? '已收录成交事实' : '尚未收录'}<small>不代表已对账</small></td><td><a className="detail-link" href={`/strategies/${strategy}/trades/${trade.id}`} aria-label={`查看成交 ${trade.id}`}>查看 ↗</a></td></tr>)}</tbody></table></div>}
        <nav className="pagination" aria-label="成交分页"><button className="button secondary" disabled={query.loading || cursors.length === 1} onClick={() => setCursors(cursors.slice(0, -1))}>上一页成交</button><span>第 {cursors.length} 页</span><button className="button secondary" disabled={query.loading || query.data.next_after_id === '0'} onClick={() => setCursors([...cursors, query.data!.next_after_id])}>下一页成交</button></nav></>}
    </section></>;
}
export function TradeWorkspace({ onExpired }: { onExpired: () => void }) {
  const match = /^\/strategies\/([1-9][0-9]*)\/trades(?:\/([1-9][0-9]*))?$/.exec(location.pathname);
  try { if (!match) throw new Error(); integer(match[1]); if (match[2]) integer(match[2]); } catch { return <p className="error" role="alert">成交路径无效。</p>; }
  return <div className="trade-workspace">{match[2] ? <TradeDetails strategy={match[1]} id={match[2]} onExpired={onExpired} /> : <TradeList strategy={match[1]} onExpired={onExpired} />}</div>;
}
