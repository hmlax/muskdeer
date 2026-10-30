import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import type { Session } from './api';
import { cancellationStates, getCancellation, requestCancellation } from './cancellation-api';
import type { CancellationReceipt, CancellationRequest } from './cancellation-api';
import type { Order } from './order-api';
import { integer } from './strategy-api';

function utc(value: string): string {
  const ms = Number(value); return Number.isSafeInteger(ms) && ms <= 8640000000000000 ? new Date(ms).toISOString() : value;
}
function locationFor(strategy: string, id: string): string { return `/strategies/${strategy}/order-cancellations/${id}`; }
function useCancellation(strategy: string, id: string, order: string | null, onExpired: () => void) {
  const [data, setData] = useState<CancellationReceipt | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(false); const [polling, setPolling] = useState(false); const [epoch, setEpoch] = useState(0);
  useEffect(() => {
    let active = true; let call: AbortController | null = null; let next: ReturnType<typeof setTimeout> | undefined; let timeout: ReturnType<typeof setTimeout> | undefined;
    const end = Date.now() + 20000; setData(null); setError(''); setPolling(true);
    async function read() {
      if (!active) return;
      const remaining = end - Date.now(); if (remaining <= 0) { setPolling(false); return; }
      call = new AbortController(); timeout = setTimeout(() => call?.abort(), Math.min(6000, remaining)); setLoading(true);
      try {
        const result = await getCancellation(strategy, id, call.signal);
        if (!active) return;
        if (order !== null && result.order_id !== order) throw new ApiError('INVALID_RESPONSE');
        setData(result);
        if (result.state === 'ACCEPTED' && Date.now() < end) next = setTimeout(read, Math.min(1000, end - Date.now()));
        else setPolling(false);
      } catch (cause) {
        if (!active) return;
        setData(null); setPolling(false);
        if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
        const code = cause instanceof ApiError ? cause.code : '';
        setError(code === 'NOT_FOUND' ? '找不到此撤单收据。' : code === 'FORBIDDEN' ? '当前账户无权读取撤单结果。' : '暂时无法确认撤单结果，请重新查询。');
      } finally { clearTimeout(timeout); if (active) setLoading(false); }
    }
    void read();
    return () => { active = false; call?.abort(); clearTimeout(next); clearTimeout(timeout); };
  }, [strategy, id, order, onExpired, epoch]);
  return { data, error, loading, polling, refresh: () => setEpoch(value => value + 1) };
}

function CancellationRecord({ receipt }: { receipt: CancellationReceipt }) {
  return <div className="cancellation-record" aria-label="撤单记录"><div className="card-heading"><h4>撤单收据</h4><span className={`state-badge ${receipt.state === 'SUCCEEDED' ? 'running' : ''}`}>{cancellationStates[receipt.state]}</span></div>
    <p className="notice">{receipt.state === 'ACCEPTED' ? '请求已保存；尚未确认订单终止，资金义务以 OMS 和核心事实为准。' : receipt.state === 'FAILED' ? '此请求已明确失败，仍需核对未完成订单与预留。' : receipt.observation?.state === 'FILLED' ? '订单已成交结束；此结果不表示已撤销成交。' : 'OMS 已确认此订单终态；其他订单、资金与策略状态请分别查询。'}</p>
    <dl className="parameter-details">{([['撤单收据 ID', receipt.id], ['策略 ID', receipt.strategy_id], ['目标订单 ID', receipt.order_id], ['确认的订单修订', receipt.expected_order_revision], ['提交原因', receipt.reason], ['结果代码', receipt.outcome_code], ['收据修订', receipt.revision], ['受理时间', utc(receipt.created_at_ms)], ...(receipt.observation ? [['已确认 OMS 状态', receipt.observation.state], ['已确认 OMS 修订', receipt.observation.order_revision], ['完成时间', utc(receipt.completed_at_ms!)]] : [])] as [string, string][]).map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
    <a className="detail-link" href={`/audits?action=order.cancel&resource=${encodeURIComponent(`strategy_order:${receipt.order_id}`)}`}>查看撤单审计 ↗</a>
  </div>;
}

function CancellationResult({ strategy, id, order, onExpired, onTerminal }: { strategy: string; id: string; order: string | null; onExpired: () => void; onTerminal?: () => void }) {
  const result = useCancellation(strategy, id, order, onExpired); const notified = useRef(false); const callback = useRef(onTerminal);
  useEffect(() => { callback.current = onTerminal; }, [onTerminal]);
  useEffect(() => { if (result.data && result.data.state !== 'ACCEPTED' && !notified.current) { notified.current = true; callback.current?.(); } }, [result.data]);
  return <section className="cancellation-result" aria-label="撤单结果">
    {result.loading && !result.data && <p role="status">正在读取撤单结果…</p>}
    {result.error && <p className="error" role="alert">{result.error}</p>}
    {result.data && <CancellationRecord receipt={result.data} />}
    <div className="operation-links"><a className="detail-link" href={locationFor(strategy, id)}>打开撤单收据 ↗</a><button className="button secondary compact" disabled={result.loading} onClick={result.refresh}>查询撤单结果</button></div>
    {result.polling && <p className="muted" role="status">正在等待订单终态，自动查询最多20秒。</p>}
    {!result.polling && result.data?.state === 'ACCEPTED' && <p className="notice">订单仍待确认，可稍后手动查询；离开页面不会撤回请求。</p>}
  </section>;
}

export function OrderCancellation({ strategy, orderID, current, session, onExpired, onRefresh, onLock }: { strategy: string; orderID: string; current: Order | null; session: Session; onExpired: () => void; onRefresh: () => void; onLock: (value: boolean) => void }) {
  const [basis, setBasis] = useState<Order | null>(null); const [reason, setReason] = useState(''); const [confirmed, setConfirmed] = useState(false);
  const [prepared, setPrepared] = useState<CancellationRequest | null>(null); const [receipt, setReceipt] = useState<CancellationReceipt | null>(null); const [busy, setBusy] = useState(false); const [error, setError] = useState('');
  const write = useRef<AbortController | null>(null); const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; write.current?.abort(); }; }, []);
  useEffect(() => { onLock(busy || prepared !== null); return () => onLock(false); }, [busy, prepared, onLock]);
  const canWrite = session.strategy_controls_enabled && session.role !== 'viewer'; const terminal = current && ['FILLED', 'CANCELED', 'REJECTED'].includes(current.state);
  const stale = !current || basis !== null && (basis.id !== current.id || basis.revision !== current.revision);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (busy || write.current || !basis || !canWrite) return;
    let request = prepared;
    if (!request) {
      const note = reason.trim();
      if (!confirmed || stale || terminal || !note || note.includes('\0') || new TextEncoder().encode(note).length > 256) { setError('请重新核对当前订单、确认信息和操作原因。'); return; }
      request = { strategy_id: strategy, order_id: orderID, key: crypto.randomUUID(), body: { expected_order_revision: basis.revision, reason: note } };
      setPrepared(request); setReceipt(null);
    }
    const call = new AbortController(); write.current = call; const timer = setTimeout(() => call.abort(), 6000); setBusy(true); setError('');
    try {
      const result = await requestCancellation(request, call.signal);
      if (!mounted.current) return;
      setReceipt(result); setPrepared(null); setBasis(null); setReason(''); setConfirmed(false); onRefresh();
    } catch (cause) {
      if (!mounted.current) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      const code = cause instanceof ApiError ? cause.code : '';
      if (code === 'CONFLICT' || code === 'INVALID_REQUEST' || code === 'NOT_FOUND') {
        setPrepared(null); setBasis(null); setConfirmed(false); onRefresh();
        setError(code === 'CONFLICT' ? '订单修订或请求内容已变化，请读取当前订单后重新确认。' : code === 'NOT_FOUND' ? '未找到此策略范围内的订单。' : '撤单参数无效，请重新核对。');
      } else setError(code === 'FORBIDDEN' ? '当前会话无权撤单；原请求内容保留供核对和重试。' : '尚未确认撤单请求的受理结果，请按原请求重试；离开页面不会撤回请求。');
    } finally { clearTimeout(timer); write.current = null; if (mounted.current) setBusy(false); }
  }
  return <section className="order-cancellation" aria-label="指定订单撤单"><h4>指定订单撤单</h4><p className="notice">只撤销订单 {orderID}；仍在运行的策略可能重新报价。暂停、停止和关闭新风险请分别操作，撤单不会平仓。</p>
    {!canWrite ? <p className="muted">当前入口仅支持查询，无法提交撤单。</p> : <>
      {!basis && <button className="button secondary compact" disabled={!current || !!terminal || busy} onClick={() => { setBasis(current); setReason(''); setConfirmed(false); setError(''); }}>撤销此订单</button>}
      {terminal && !prepared && <p className="muted">此订单已是终态，请核对订单与核心事实。</p>}
      {basis && <form className="control-form" onSubmit={submit}><p>确认订单 {basis.id} · {basis.side} · {basis.price} × {basis.quantity} · 修订 {prepared?.body.expected_order_revision ?? basis.revision}</p>
        <label htmlFor="cancel-order-reason">撤单原因</label><textarea id="cancel-order-reason" required maxLength={256} disabled={busy || prepared !== null} value={prepared?.body.reason ?? reason} onChange={event => setReason(event.target.value)} />
        <label className="order-checkbox"><input type="checkbox" checked={confirmed} disabled={busy || prepared !== null} onChange={event => setConfirmed(event.target.checked)} />我确认只撤销此订单，受理不代表订单已终止</label>
        {stale && !prepared && <p className="error" role="alert">订单读取状态已变化，请取消并重新选择当前订单。</p>}
        <div className="control-actions"><button className="button primary" type="submit" disabled={busy || !prepared && (stale || !!terminal || !confirmed)}>{busy ? '正在提交撤单…' : prepared ? '按原请求重试撤单' : '确认撤销此订单'}</button>{!prepared && <button className="button secondary" type="button" disabled={busy} onClick={() => setBasis(null)}>取消撤单确认</button>}</div>
      </form>}
    </>}
    {error && <p className="error" role="alert">{error}</p>}
    {receipt && <CancellationResult key={receipt.id} strategy={strategy} id={receipt.id} order={orderID} onExpired={onExpired} onTerminal={onRefresh} />}
  </section>;
}

export function CancellationWorkspace({ onExpired }: { onExpired: () => void }) {
  const match = /^\/strategies\/([1-9][0-9]{0,18})\/order-cancellations\/([1-9][0-9]{0,18})$/.exec(location.pathname);
  try { if (!match) throw new ApiError('INVALID_RESPONSE'); integer(match[1]); integer(match[2]); }
  catch { return <p className="error" role="alert">撤单收据地址无效。</p>; }
  return <div className="strategy-workspace cancellation-workspace"><a className="back-link" href={`/strategies/${match![1]}`}>← 策略详情</a><div className="page-heading"><span className="eyebrow">ORDER CANCELLATION</span><h2>撤单结果</h2><p>核对指定订单请求的持久结果与 OMS 终态证明。</p></div><div className="card"><CancellationResult strategy={match![1]} id={match![2]} order={null} onExpired={onExpired} /></div></div>;
}
