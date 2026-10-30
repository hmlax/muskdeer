import { useCallback, useEffect, useState } from 'react';
import { OrderCancellation } from "./cancellations";
import type { Session } from "./api";
import { ApiError } from './api';
import { getOrder, listOrders, orderStates } from './order-api';
import type { Order, OrderFilters } from './order-api';
import { sources } from './strategy-api';

function useOrders<T>(load: ((signal: AbortSignal) => Promise<T>) | null, onExpired: () => void) {
  const [data, setData] = useState<T | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(false); const [revision, setRevision] = useState(0);
  useEffect(() => {
    setData(null); setError('');
    if (!load) { setLoading(false); return; }
    let active = true; let timeout = false; const controller = new AbortController();
    const timer = setTimeout(() => { timeout = true; controller.abort(); }, 6000); setLoading(true);
    load(controller.signal).then(value => { if (active) setData(value); }).catch(cause => {
      if (!active) return;
      const code = cause instanceof ApiError ? cause.code : '';
      if (code === 'UNAUTHENTICATED') { onExpired(); return; }
      setError(timeout ? '订单读取超时，请重试。' : code === 'NOT_FOUND' ? '未找到此订单或策略。' : code === 'FORBIDDEN' ? '当前账户无权查看订单。' : '暂时无法读取订单，请稍后重试。');
    }).finally(() => { clearTimeout(timer); if (active) setLoading(false); });
    return () => { active = false; clearTimeout(timer); controller.abort(); };
  }, [load, onExpired, revision]);
  return { data, error, loading, reload: () => setRevision(value => value + 1) };
}
function OrderStatus({ loading, error, retry }: { loading: boolean; error: string; retry: () => void }) {
  return loading ? <p role="status">正在读取订单…</p> : error ? <div><p className="error" role="alert">{error}</p><button className="button secondary" onClick={retry}>重试订单读取</button></div> : null;
}
function timestamp(value: string): string {
  const milliseconds = Number(value); const date = new Date(milliseconds);
  return Number.isSafeInteger(milliseconds) && !Number.isNaN(date.getTime()) ? date.toISOString() : value;
}
function OrderDetail({ order }: { order: Order }) {
  const core = order.core;
  return <section className="order-detail" aria-label="订单详情"><h4>订单详情 · {order.id}</h4><dl className="parameter-details">
    {([['客户端订单 ID', order.client_order_id], ['策略 ID', order.strategy_id], ['账户', order.account_id], ['交易对 ID', order.instrument_id], ['历史配置与来源', `v${order.config_version} · ${sources[order.market_data_source]}`], ['方向 / TIF', `${order.side} / ${order.time_in_force}`], ['只减仓', order.reduce_only ? '是' : '否'], ['价格', order.price], ['委托数量', order.quantity], ['OMS 状态 / 撤单', `${order.state} / ${order.cancel_state}`], ['OMS 已确认成交', order.filled_quantity], ['OMS 名义额预留', order.reserved_notional], ['OMS 修订', order.revision], ['已确认核心 ID / 修订', `${order.confirmed_core_id} / ${order.confirmed_core_revision}`], ['业务错误代码', order.error_code || '—'], ['截止时间', timestamp(order.deadline_ms)], ['创建时间', timestamp(order.created_at_ms)], ['OMS 更新时间', timestamp(order.updated_at_ms)]] as [string, string][]).map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}
  </dl><h4>核心最新可见事实</h4>{core ? <dl className="parameter-details">{([['核心 ID / 修订', `${core.id} / ${core.revision}`], ['核心状态 / 撤单', `${core.state} / ${core.cancel_state}`], ['核心成交数量', core.filled_quantity], ['冻结资产', core.frozen_asset], ['冻结金额', core.frozen_amount], ['报价资产费率', core.quote_fee_rate], ['核心更新时间', timestamp(core.updated_at_ms)]] as [string, string][]).map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl> : <p className="notice">尚未观察到核心记录；这不能证明订单未受理。</p>}</section>;
}
export function OrderPanel({ id, available, session, onExpired }: { id: string; available: boolean; session: Session; onExpired: () => void }) {
  const [locked, setLocked] = useState(false);
  const [open, setOpen] = useState(false); const [filters, setFilters] = useState<OrderFilters>({ state: '', openOnly: false, limit: 20 }); const [cursors, setCursors] = useState(['0']); const [selected, setSelected] = useState<string | null>(null);
  const cursor = cursors[cursors.length - 1];
  const loader = useCallback((signal: AbortSignal) => listOrders(id, cursor, filters, signal), [id, cursor, filters]);
  const detailLoader = useCallback((signal: AbortSignal) => getOrder(id, selected!, signal), [id, selected]);
  const query = useOrders(open ? loader : null, onExpired); const detail = useOrders(open && selected ? detailLoader : null, onExpired);
  function change(next: OrderFilters) { setFilters(next); setCursors(['0']); setSelected(null); }
  function page(next: string[]) { setCursors(next); setSelected(null); }
  if (!available && !open) return null;
  return <section className="card order-panel" aria-label="订单与核心事实"><div className="card-heading"><h3>订单与核心事实</h3><button className="button secondary" disabled={query.loading || locked} onClick={() => { if (open) { setSelected(null); query.reload(); } else setOpen(true); }}>{open ? '刷新订单' : '查看订单'}</button></div>
    {open && <><p className="notice">OMS 确认与核心观察分别展示；读取不会确认撤单或释放预留。核心缺失不能证明未受理。</p><div className="order-filters">
      <label>OMS 状态<select disabled={locked} aria-label="OMS 状态" value={filters.state} onChange={event => change({ ...filters, state: event.target.value, openOnly: filters.openOnly && !['FILLED', 'CANCELED', 'REJECTED'].includes(event.target.value) })}><option value="">全部状态</option>{orderStates.map(state => <option key={state}>{state}</option>)}</select></label>
      <label>每页订单<select disabled={locked} aria-label="每页订单" value={filters.limit} onChange={event => change({ ...filters, limit: Number(event.target.value) })}>{[1, 20, 50].map(limit => <option key={limit}>{limit}</option>)}</select></label>
      <label className="order-checkbox"><input type="checkbox" disabled={locked} checked={filters.openOnly} onChange={event => change({ ...filters, openOnly: event.target.checked, state: event.target.checked && ['FILLED', 'CANCELED', 'REJECTED'].includes(filters.state) ? '' : filters.state })} />仅开放义务（含 UNKNOWN）</label>
    </div><OrderStatus loading={query.loading} error={query.error} retry={query.reload} />
    {query.data && <>{query.data.orders.length === 0 ? <p className="empty-state">没有匹配的订单</p> : <div className="table-scroll" role="region" aria-label="订单列表" tabIndex={0}><table><thead><tr><th>OMS ID / 历史来源</th><th>方向 / 价格 / 数量</th><th>OMS 状态 / 预留</th><th>核心状态 / 冻结</th><th>详情</th></tr></thead><tbody>{query.data.orders.map(order => <tr key={order.id}><td>{order.id}<small>v{order.config_version} · {sources[order.market_data_source]}</small></td><td>{order.side}<small>{order.price} × {order.quantity}</small></td><td>{order.state}<small>{order.cancel_state} · {order.reserved_notional}</small></td><td>{order.core?.state ?? '尚未观察到'}<small>{order.core ? `${order.core.frozen_amount} ${order.core.frozen_asset}` : '不能据此判定未受理'}</small></td><td><button className="detail-link link-button" disabled={locked} onClick={() => { setSelected(order.id); detail.reload(); }} aria-label={`查看订单 ${order.id}`}>查看 ↗</button></td></tr>)}</tbody></table></div>}
      <nav className="pagination" aria-label="订单分页"><button className="button secondary" disabled={query.loading || locked || cursors.length === 1} onClick={() => page(cursors.slice(0, -1))}>上一页订单</button><span>第 {cursors.length} 页</span><button className="button secondary" disabled={query.loading || locked || query.data.next_after_id === '0'} onClick={() => page([...cursors, query.data!.next_after_id])}>下一页订单</button></nav></>}
    {selected && <><OrderStatus loading={detail.loading} error={detail.error} retry={detail.reload} />{detail.data && <OrderDetail order={detail.data} />}<OrderCancellation key={selected} strategy={id} orderID={selected} current={detail.data} session={session} onExpired={onExpired} onRefresh={() => { detail.reload(); query.reload(); }} onLock={setLocked} /></>}</>}
  </section>;
}
