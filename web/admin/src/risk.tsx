import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import type { Session } from './api';
import { changeRisk, getRiskState, riskActions } from './risk-api';
import type { RiskAction, RiskReceipt, RiskRequest, RiskState } from './risk-api';
import { sources, states } from './strategy-api';
import type { Strategy } from './strategy-api';

function utc(value: string): string {
  const ms = Number(value);
  return Number.isSafeInteger(ms) && ms <= 8640000000000000 ? new Date(ms).toISOString() : '—';
}

export function RiskPanel({ id, current, loading, session, onExpired, onRefresh }: { id: string; current: Strategy | undefined; loading: boolean; session: Session; onExpired: () => void; onRefresh: () => void }) {
  const [open, setOpen] = useState(false); const [epoch, setEpoch] = useState(0);
  const [state, setState] = useState<RiskState | null>(null); const [reading, setReading] = useState(false); const [readError, setReadError] = useState('');
  const [basis, setBasis] = useState<RiskState | null>(null); const [action, setAction] = useState<RiskAction | null>(null);
  const [reason, setReason] = useState(''); const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  const [prepared, setPrepared] = useState<RiskRequest | null>(null); const [receipt, setReceipt] = useState<RiskReceipt | null>(null);
  const write = useRef<AbortController | null>(null); const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; write.current?.abort(); }; }, []);
  useEffect(() => {
    if (!open) return;
    let active = true; let timedOut = false;
    const call = new AbortController(); const timer = setTimeout(() => { timedOut = true; call.abort(); }, 6000);
    setReading(true); setReadError(''); setState(null);
    getRiskState(id, call.signal).then(value => { if (active) setState(value); }).catch(cause => {
      if (!active) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      const code = cause instanceof ApiError ? cause.code : '';
      setReadError(code === 'NOT_FOUND' ? '此策略尚无可用的风险预算。' : code === 'FORBIDDEN' ? '当前账户无权读取风险状态。' : timedOut ? '风险状态读取超时，请重试。' : '暂时无法读取风险状态，请重试。');
    }).finally(() => { clearTimeout(timer); if (active) setReading(false); });
    return () => { active = false; clearTimeout(timer); call.abort(); };
  }, [id, open, epoch, onExpired]);
  const canWrite = session.strategy_controls_enabled && session.role !== 'viewer';
  const stale = basis !== null && (state !== null && (basis.revision !== state.revision || basis.requested_config_version !== state.requested_config_version || basis.desired_state !== state.desired_state) ||
    current !== undefined && (basis.requested_config_version !== current.requested_config_version || basis.desired_state !== current.desired_state));
  function select(value: RiskAction) {
    if (!state || reading || busy || prepared) return;
    setBasis(state); setAction(value); setReason(''); setError('');
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (busy || write.current || !basis || !action) return;
    let request = prepared;
    if (!request) {
      const note = reason.trim();
      if (loading || reading || !state || stale || !note || new TextEncoder().encode(note).length > 256 || note.includes('\0') || action === 'risk.strategy.unblock' && session.role !== 'admin') { setError('请核对当前风险版本、确认信息、权限和操作原因。'); return; }
      request = { strategy_id: id, key: crypto.randomUUID(), body: { action, expected_revision: basis.revision, reason: note } };
      if (action === 'risk.strategy.unblock') { request.body.expected_config_version = basis.requested_config_version; request.body.expected_desired_state = basis.desired_state; }
      setPrepared(request); setReceipt(null);
    }
    const call = new AbortController(); write.current = call; const timer = setTimeout(() => call.abort(), 6000); setBusy(true); setError('');
    try {
      const value = await changeRisk(request, call.signal);
      if (!mounted.current) return;
      setReceipt(value); setPrepared(null); setAction(null); setBasis(null); setReason(''); setEpoch(value => value + 1); onRefresh();
    } catch (cause) {
      if (!mounted.current) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      const code = cause instanceof ApiError ? cause.code : '';
      if (code === 'CONFLICT' || code === 'RECOVERY_DENIED' || code === 'INVALID_REQUEST') {
        setPrepared(null); setAction(null); setBasis(null); setEpoch(value => value + 1); onRefresh();
        setError(code === 'CONFLICT' ? '风险版本、配置或运行目标已变化，请重新读取并确认。' : code === 'RECOVERY_DENIED' ? '恢复条件未满足。请核对旧订单、预留、对账、额度及所选行情，再读取状态并重新确认。' : '请求条件无效，请重新核对后确认。');
      } else setError(code === 'FORBIDDEN' ? '当前会话无权执行此操作，原请求已保留。' : '尚未确认操作结果，请按原请求重试；离开页面不会撤回请求。');
    } finally { clearTimeout(timer); write.current = null; if (mounted.current) setBusy(false); }
  }
  if (!current && !open) return null;
  return <section className="card risk-card" aria-label="策略风险与额度" aria-busy={reading || busy}>
    <div className="card-heading"><h3>策略风险与额度</h3><span className="badge">{canWrite ? session.role === 'admin' ? '管理员' : '操作员' : '只读访问'}</span></div>
    <p className="notice">管理本策略的新风险资格。订单与资金以核心确认结果为准，恢复开关后按运行目标和当前风控执行。</p>
    {!open ? <button type="button" className="button secondary" onClick={() => setOpen(true)}>查看风险状态</button> : <>
      <div className="risk-toolbar"><button type="button" className="button secondary compact" disabled={reading || busy} onClick={() => setEpoch(value => value + 1)}>刷新风险状态</button><a className="detail-link" href={`/audits?resource=${encodeURIComponent(`strategy_risk:${id}`)}`}>查看风险审计 ↗</a></div>
      {reading && <p role="status">正在读取风险状态…</p>}
      {readError && <p className="error" role="alert">{readError}</p>}
      {state && <div className="risk-current" aria-label="当前风险快照"><div className="card-heading"><h4>当前风险快照</h4><span className={`state-badge ${state.enabled ? 'running' : ''}`}>{state.enabled ? '资格开关已开启' : '资格开关已关闭'}</span></div>
        <dl className="parameter-details"><div><dt>风险管理版本</dt><dd>{state.revision}</dd></div><div><dt>策略额度（{state.quote_asset}）</dt><dd>{state.limit_notional}</dd></div><div><dt>当前预留（{state.quote_asset}）</dt><dd>{state.reserved_notional}</dd></div><div><dt>实际状态</dt><dd>{states[state.state]}</dd></div><div><dt>请求目标</dt><dd>{states[state.desired_state]}</dd></div><div><dt>请求配置 / 行情</dt><dd>v{state.requested_config_version} · {sources[state.market_data_source]}</dd></div><div><dt>预算更新时间（UTC）</dt><dd>{utc(state.updated_at_ms)}</dd></div></dl>
        {canWrite && !action && <div className="control-actions"><button type="button" className="button secondary stop-action" disabled={busy || reading || loading || !state.enabled} onClick={() => select('risk.strategy.block')}>关闭新风险</button>{session.role === 'admin' && <button type="button" className="button secondary" disabled={busy || reading || loading || state.enabled} onClick={() => select('risk.strategy.unblock')}>恢复新风险</button>}</div>}
        {!canWrite && <p className="muted">当前账户可查看风险状态及审计记录。</p>}{canWrite && session.role !== 'admin' && !state.enabled && <p className="muted">恢复新风险需要管理员重新核验当前条件。</p>}
      </div>}
      {action && basis && <form className="control-form" aria-label="确认风险操作" onSubmit={submit}><h4>确认{riskActions[action]}</h4><p>策略 {id} · 风险管理版本 {prepared?.body.expected_revision ?? basis.revision}</p>
        {action === 'risk.strategy.unblock' ? <p className="notice">确认请求配置 v{basis.requested_config_version} · {sources[basis.market_data_source]}，运行目标「{states[basis.desired_state]}」。服务端将重新核验旧订单、资金预留、对账、额度及当前所选行情。</p> : <p className="notice">关闭本策略的新风险资格。已有订单的预留和冻结继续等待核心终态确认。</p>}
        {stale && !prepared && <p className="notice" role="status">确认基线已变化，请取消本次确认并刷新风险状态。</p>}
        <label>风险操作原因<textarea aria-label="风险操作原因" required maxLength={256} disabled={busy || prepared !== null} value={prepared?.body.reason ?? reason} onChange={event => setReason(event.target.value)} /></label>
        <div className="control-actions"><button className="button primary" type="submit" disabled={busy || !prepared && (reading || loading || !state || stale)}>{busy ? '正在提交风险操作…' : prepared ? '按原请求重试风险操作' : `确认${riskActions[action]}`}</button>{!prepared && <button className="button secondary" type="button" disabled={busy} onClick={() => { setAction(null); setBasis(null); setError(''); }}>取消风险操作</button>}</div>
      </form>}
      {error && <p className="error" role="alert">{error}</p>}
      {receipt && <div className="risk-receipt" aria-label="风险操作收据"><h4>风险操作收据</h4><p role="status">已记录「{riskActions[receipt.action]}」，请核对当前风险快照。</p><dl className="parameter-details"><div><dt>收据 ID</dt><dd>{receipt.id}</dd></div><div><dt>管理版本</dt><dd>{receipt.previous_revision} → {receipt.revision}</dd></div><div><dt>提交原因</dt><dd>{receipt.reason}</dd></div><div><dt>记录时间（UTC）</dt><dd>{utc(receipt.created_at_ms)}</dd></div></dl>{state && BigInt(state.revision) > BigInt(receipt.revision) && <p className="notice">当前状态已有后续变更，此收据保留原操作记录。</p>}</div>}
    </>}
  </section>;
}
