import { useCallback, useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import { integer, sources } from './strategy-api';
import { defaultMarketFilters, getMarket, initialMarketFilters, listMarkets, marketFilterURL, marketQualities, validMarketFilters } from './market-api';
import type { Market, MarketFilters, MarketSnapshot } from './market-api';

const reasons: Record<string, string> = { SYNCHRONIZING: '等待完整快照', DISCONNECTED: '发布者失效或连接中断', RECOVERY_SAMPLES: '等待恢复样本', SEQUENCE: '序列缺口', PAYLOAD: '数据格式异常', OVERFLOW: '缓冲区溢出', AGE: '行情超过时效', PRICE_JUMP: '价格跳变', SPREAD: '价差超限', FUTURE_TIME: '事件时间超前', CLOCK_REVERSED: '时钟回退' };
function reason(value: string): string { return value ? `${reasons[value] ?? value} · ${value}` : '无'; }
function utc(value: string): string {
  // Integer timestamps only. Financial values and identities stay strings.
  if (value === '0') return '—'; const n = Number(value); return Number.isSafeInteger(n) && n <= 8640000000000000 ? new Date(n).toISOString() : value;
}
function elapsed(m: Market, clock: number): number { return Math.max(0, Math.floor(Math.max(clock, performance.now()) - m.started)); }
function fresh(m: Market, clock: number): boolean {
  const s = m.snapshot;
  if (!s || s.reading.quality !== 'HEALTHY' || !s.publisher_current) return false;
  const now = BigInt(m.evaluated_at_ms) + BigInt(elapsed(m, clock));
  return BigInt(s.reading.valid_until_ms) > now && BigInt(s.publisher_expires_at_ms) > now;
}
function Status({ market, clock }: { market: Market; clock: number }) {
  const s = market.snapshot; const healthy = fresh(market, clock);
  return <span className={`state-badge ${healthy ? 'running' : 'market-unavailable'}`}>{!s ? '尚无快照' : s.reading.quality === 'HEALTHY' && !healthy ? '本次读数已过期' : marketQualities[s.reading.quality]}</span>;
}
function useMarketQuery<T>(load: ((signal: AbortSignal) => Promise<T>) | null, onExpired: () => void) {
  const [data, setData] = useState<T | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [epoch, setEpoch] = useState(0);
  useEffect(() => {
    setData(null); setError('');
    if (!load) { setLoading(false); setError('行情筛选或地址无效，请重新选择。'); return; }
    let active = true; let timedOut = false; const call = new AbortController(); const timer = setTimeout(() => { timedOut = true; call.abort(); }, 6000); setLoading(true);
    load(call.signal).then(value => { if (active) setData(value); }).catch(cause => {
      if (!active) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      const code = cause instanceof ApiError ? cause.code : '';
      setError(timedOut ? '读取超时，请重试。' : code === 'NOT_FOUND' ? '未找到此行情绑定或交易对。' : code === 'FORBIDDEN' ? '当前账户无权读取行情监控。' : '暂时无法读取行情监控，请稍后重试。');
    }).finally(() => { clearTimeout(timer); if (active) setLoading(false); });
    return () => { active = false; call.abort(); clearTimeout(timer); };
  }, [load, onExpired, epoch]);
  return { data, error, loading, refresh: () => setEpoch(value => value + 1) };
}
function QueryStatus({ loading, error, retry }: { loading: boolean; error: string; retry: () => void }) {
  if (loading) return <div className="card loading" role="status"><span className="spinner" />正在读取行情监控…</div>;
  return error ? <div className="card"><p className="error" role="alert">{error}</p><button className="button secondary" onClick={retry}>重试</button></div> : null;
}
function Fields({ values }: { values: [string, string][] }) {
  return <dl className="parameter-details">{values.map(([name, value]) => <div key={name}><dt>{name}</dt><dd>{value}</dd></div>)}</dl>;
}
export function MarketWorkspace({ onExpired }: { onExpired: () => void }) {
  const [clock, setClock] = useState(() => performance.now());
  useEffect(() => {
    const update = () => setClock(performance.now()); const timer = setInterval(update, 250);
    document.addEventListener('visibilitychange', update); window.addEventListener('pageshow', update);
    return () => { clearInterval(timer); document.removeEventListener('visibilitychange', update); window.removeEventListener('pageshow', update); };
  }, []);
  const id = /^\/markets\/([1-9][0-9]{0,18})$/.exec(location.pathname)?.[1];
  return id ? <MarketDetail id={id} clock={clock} onExpired={onExpired} /> : <MarketList clock={clock} onExpired={onExpired} />;
}
function MarketList({ onExpired, clock }: { onExpired: () => void; clock: number }) {
  const [filters, setFilters] = useState(initialMarketFilters); const [draft, setDraft] = useState<MarketFilters>(() => filters ?? defaultMarketFilters); const [validation, setValidation] = useState(''); const [cursors, setCursors] = useState(['0']); const cursor = cursors[cursors.length - 1];
  const loader = useCallback((signal: AbortSignal) => listMarkets(filters ?? defaultMarketFilters, cursor, signal), [filters, cursor]); const query = useMarketQuery(filters ? loader : null, onExpired);
  function apply(next: MarketFilters) {
    if (!validMarketFilters(next)) { setValidation('交易对 ID 必须是规范正整数；请选择有效模式与来源。'); return; }
    setValidation(''); setDraft(next); setFilters(next); setCursors(['0']); query.refresh(); history.replaceState(null, '', `/markets?${marketFilterURL(next)}`);
  }
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); apply({ ...draft }); }
  return <div className="strategy-workspace market-workspace" aria-busy={query.loading}><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">MARKET OBSERVATIONS</span><h2>行情监控</h2><p>按来源与交易对核对质量、时效和发布事实。</p></div><button className="button secondary compact" disabled={query.loading} onClick={query.refresh}>刷新行情</button></div>
    <form className="card market-filter-card" aria-label="行情筛选" onSubmit={submit}><div className="market-filters">
      <label>行情模式<select aria-label="行情模式" value={draft.mode} onChange={event => setDraft({ ...draft, mode: event.target.value as MarketFilters['mode'] })}><option value="LIVE">LIVE · 实时公共行情</option><option value="REPLAY">REPLAY · 模拟回放</option></select></label>
      <label>行情来源<select aria-label="行情来源" value={draft.source} onChange={event => setDraft({ ...draft, source: event.target.value as MarketFilters['source'] })}><option value="">全部来源</option>{Object.entries(sources).map(([id, name]) => <option key={id} value={id}>{name}</option>)}</select></label>
      <label>交易对 ID（精确）<input aria-label="交易对 ID（精确）" inputMode="numeric" maxLength={19} value={draft.instrument} onChange={event => setDraft({ ...draft, instrument: event.target.value })} placeholder="留空查询全部交易对" /></label>
      <label>每页行情<select aria-label="每页行情" value={draft.limit} onChange={event => setDraft({ ...draft, limit: Number(event.target.value) })}>{[1, 20, 50].map(n => <option key={n}>{n}</option>)}</select></label>
    </div>{validation && <p className="error" role="alert">{validation}</p>}<div className="audit-filter-actions"><button className="button primary" type="submit">查询行情</button><button className="button secondary" type="button" onClick={() => apply({ ...defaultMarketFilters, mode: draft.mode })}>清空来源与交易对</button></div></form>
    <p className="notice market-notice">LIVE 与 REPLAY 分开查询；读取时健康不代表策略获准交易。页面保留本次采样，时效到期后须刷新，不自动换源。</p>
    <QueryStatus loading={query.loading} error={query.error} retry={query.refresh} />
    {query.data && <><div className="card data-card"><div className="card-heading"><h3>行情列表</h3><span className="badge">本页 {query.data.markets.length} 条</span></div>
      {query.data.markets.length === 0 ? <div className="empty-state"><h3>没有匹配的行情绑定</h3><p>可调整模式、来源和交易对后重新查询。</p></div> : <div className="table-scroll" role="region" aria-label="行情列表" tabIndex={0}><table><thead><tr><th>来源 / 产品</th><th>模式 / 当前读数</th><th>采样中价</th><th>序列 / 评估时间</th><th>详情</th></tr></thead><tbody>{query.data.markets.map(m => <tr key={m.feed.id}>
        <td>{sources[m.feed.source]} · {m.instrument.symbol}<small>{m.feed.external_symbol} · Feed {m.feed.id}</small><small>交易对 {m.instrument.id}</small></td><td>{m.feed.mode}<small><Status market={m} clock={clock} /></small><small>{m.snapshot ? reason(m.snapshot.reading.reason) : '未观察到快照'}</small></td><td>{m.snapshot?.reading.quality === 'HEALTHY' ? m.snapshot.reading.mid_price : '—'}<small>{m.instrument.quote_asset} / {m.instrument.base_asset} · 读取时样本</small></td><td>{m.snapshot?.reading.sequence ?? '—'}<small>{utc(m.evaluated_at_ms)}</small></td><td><a className="detail-link" aria-label={`查看行情 ${m.feed.id}`} href={`/markets/${m.feed.id}`}>查看 ↗</a></td>
      </tr>)}</tbody></table></div>}</div><nav className="pagination" aria-label="行情分页"><button className="button secondary" disabled={query.loading || cursors.length === 1} onClick={() => setCursors(cursors.slice(0, -1))}>上一页行情</button><span>第 {cursors.length} 页</span><button className="button secondary" disabled={query.loading || query.data.next_after_id === '0'} onClick={() => setCursors([...cursors, query.data!.next_after_id])}>下一页行情</button></nav></>}
  </div>;
}
function MarketDetail({ id, clock, onExpired }: { id: string; clock: number; onExpired: () => void }) {
  let valid = true; try { integer(id); } catch { valid = false; }
  const loader = useCallback((signal: AbortSignal) => getMarket(id, signal), [id]); const query = useMarketQuery(valid ? loader : null, onExpired); const m = query.data;
  return <div className="strategy-workspace market-workspace" aria-busy={query.loading}><a className="back-link" href={m ? `/markets?${marketFilterURL({ ...defaultMarketFilters, mode: m.feed.mode, source: m.feed.source, instrument: m.instrument.id })}` : '/markets'}>← 行情监控</a><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">SOURCE DETAIL</span><h2>行情详情</h2><p>当前读数与历史发布诊断分别核对；采样不会授予交易权限。</p></div><button className="button secondary compact" disabled={query.loading} onClick={query.refresh}>刷新行情</button></div><QueryStatus loading={query.loading} error={query.error} retry={query.refresh} />
    {m && <><section className="card market-card" aria-label="行情详情"><div className="card-heading"><h3>{sources[m.feed.source]} · {m.instrument.symbol}</h3><Status market={m} clock={clock} /></div><Fields values={[
      ['行情绑定 ID', m.feed.id], ['交易对 ID', m.instrument.id], ['外部符号', m.feed.external_symbol], ['行情模式', m.feed.mode], ['来源版本', m.feed.source_version], ['执行场所', m.instrument.execution_venue], ['产品状态', m.instrument.status], ['元数据版本', m.instrument.metadata_version], ['价格步长', m.instrument.price_tick], ['数量步长', m.instrument.quantity_step], ['最小名义额', m.instrument.min_notional], ['数据库评估时间', utc(m.evaluated_at_ms)], ['距本次请求开始', `${elapsed(m, clock)} ms`],
    ]} /></section>{m.snapshot ? <SnapshotDetails market={m} snapshot={m.snapshot} clock={clock} /> : <div className="card market-card"><p className="notice">绑定已配置，但尚无可见快照；这不表示健康，也不会自动使用其他来源。</p></div>}
    <section className="card market-card" aria-label="质量政策"><div className="card-heading"><h3>质量政策</h3></div><Fields values={[
      ['最大行情年龄', `${m.feed.policy.maximum_age_ms} ms`], ['最大时钟偏差', `${m.feed.policy.maximum_skew_ms} ms`], ['最大价差', `${m.feed.policy.maximum_spread_bps} bps`], ['最大跳价', `${m.feed.policy.maximum_jump_bps} bps`], ['恢复样本数', String(m.feed.policy.recovery_samples)], ['恢复观察窗', `${m.feed.policy.recovery_window_ms} ms`],
    ]} /></section></>}
  </div>;
}
function SnapshotDetails({ market: m, snapshot: s, clock }: { market: Market; snapshot: MarketSnapshot; clock: number }) {
  const r = s.reading; const price = (value: string) => r.quality === 'HEALTHY' ? value : '—';
  return <><section className="card market-card" aria-label="当前行情读数"><div className="card-heading"><h3>本次行情采样</h3><Status market={m} clock={clock} /></div><p className="notice">以下价格为本次读取样本；过期样本不能作为当前参考价，请刷新。</p><Fields values={[
    ['读取时质量', marketQualities[r.quality]], ['异常原因', reason(r.reason)], ['采样买一价', price(r.bid_price)], ['采样买一数量', price(r.bid_quantity)], ['采样卖一价', price(r.ask_price)], ['采样卖一数量', price(r.ask_quantity)], ['采样中价', price(r.mid_price)], ['连接 / 订单簿代次', `${r.connection} / ${r.generation}`], ['行情序列', r.sequence], ['事件时间', utc(r.event_time_ms)], ['接收时间', utc(r.received_at_ms)], ['读数有效期至', utc(r.valid_until_ms)], ['发布者租约到期', utc(s.publisher_expires_at_ms)], ['读取时发布者有效', s.publisher_current ? '是' : '否'],
  ]} /></section><section className="card market-card" aria-label="发布诊断"><div className="card-heading"><h3>历史发布诊断</h3><span className="badge">不等于当前健康</span></div><Fields values={[
    ['发布时质量', marketQualities[s.published_quality]], ['发布时原因', reason(s.published_reason)], ['发布者 epoch', s.publisher_epoch], ['快照修订', s.revision], ['发布时间', utc(s.published_at_ms)], ['原发布有效期至', utc(s.published_valid_until_ms)],
  ]} /></section></>;
}
