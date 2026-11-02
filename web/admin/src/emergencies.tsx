import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import type { Session } from './api';
import { emergencyStates, getEmergency, listEmergencies, requestEmergency } from './emergency-api';
import type { EmergencyHistory, EmergencyObservation, EmergencyRecord, EmergencyRequest } from './emergency-api';
import { getRiskState } from './risk-api';
import type { RiskState } from './risk-api';
import { getStrategy, integer } from './strategy-api';
import type { Strategy } from './strategy-api';

function utc(value: string): string {
  const ms = Number(value); return Number.isSafeInteger(ms) && ms <= 8640000000000000 ? new Date(ms).toISOString() : value;
}
function pathFor(strategy: string, id?: string): string { return `/strategies/${strategy}/emergency-cancellations${id ? `/${id}` : ''}`; }
function readError(cause: unknown): string {
  const code = cause instanceof ApiError ? cause.code : '';
  return code === 'NOT_FOUND' ? '找不到此策略范围或撤单记录。' : code === 'FORBIDDEN' ? '当前账户无权读取范围撤单。' : '范围结果暂时不可用，请重新查询。';
}
function RecordFields({ value }: { value: EmergencyRecord }) {
  const accepted = value.acceptance;
  return <div className="cancellation-record" aria-label="范围撤单记录"><div className="card-heading"><h3>范围任务结果</h3><span className={`state-badge ${value.state === 'SUCCEEDED' ? 'running' : ''}`}>{emergencyStates[value.state]}</span></div>
    <p className="notice">{value.state === 'ACCEPTED' ? '范围已受理；目标收录或订单归结尚未完成。确认前保留资金义务。' : value.state === 'FAILED' ? '记录的任务结果有失败；后续订单事实可能继续变化，请结合当前计数与订单核对。' : '此范围已有持久归结证明，目标可能通过成交而结束。持仓和当前风险开关请分别查询。'}</p>
    <dl className="parameter-details">{([['范围记录 ID', accepted.id], ['范围', '指定策略'], ['策略 ID', accepted.strategy_id], ['确认的风险修订', accepted.expected_risk_revision], ['受理后的风险修订', accepted.risk_revision], ['操作原因', accepted.reason], ['受理时间（UTC）', utc(accepted.created_at_ms)], ['任务修订', value.revision], ['目标收录', value.enrollment_complete ? '已完成' : '尚未完成'], ...(value.completed_at_ms === null ? [] : [['完成时间（UTC）', utc(value.completed_at_ms)]])] as [string, string][]).map(([label, text]) => <div key={label}><dt>{label}</dt><dd>{text}</dd></div>)}</dl>
    <a className="detail-link" href={`/audits?action=strategy.emergency_cancel&resource=${encodeURIComponent(`strategy:${accepted.strategy_id}`)}`}>查看范围审计 ↗</a>
  </div>;
}
function EmergencyResult({ strategy, id, onExpired }: { strategy: string; id: string; onExpired: () => void }) {
  const [data, setData] = useState<EmergencyObservation | null>(null); const [error, setError] = useState('');
  const [loading, setLoading] = useState(false); const [polling, setPolling] = useState(false); const [epoch, setEpoch] = useState(0);
  useEffect(() => {
    let active = true; let call: AbortController | null = null; let next: ReturnType<typeof setTimeout> | undefined; let timeout: ReturnType<typeof setTimeout> | undefined;
    const end = Date.now() + 20000; setData(null); setError(''); setPolling(true);
    async function read() {
      if (!active || Date.now() >= end) { if (active) setPolling(false); return; }
      call = new AbortController(); timeout = setTimeout(() => call?.abort(), Math.min(6000, end - Date.now())); setLoading(true);
      try {
        const result = await getEmergency(strategy, id, call.signal); if (!active) return;
        setData(result);
        if (result.record.state === 'ACCEPTED' && Date.now() < end) next = setTimeout(read, Math.min(1000, end - Date.now()));
        else setPolling(false);
      } catch (cause) {
        if (!active) return;
        setData(null); setPolling(false);
        if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
        setError(readError(cause));
      } finally { clearTimeout(timeout); if (active) setLoading(false); }
    }
    void read(); return () => { active = false; call?.abort(); clearTimeout(next); clearTimeout(timeout); };
  }, [strategy, id, onExpired, epoch]);
  return <><div className="workspace-heading"><div className="page-heading"><span className="eyebrow">STRATEGY CANCELLATION</span><h2>范围撤单结果</h2><p>存储任务结果与当前目标事实分别展示。</p></div><button className="button secondary compact" disabled={loading} onClick={() => setEpoch(value => value + 1)}>刷新范围结果</button></div>
    {loading && !data && <p role="status">正在读取范围结果…</p>}{error && <p className="error" role="alert">{error}</p>}
    {data && <><section className="card"><RecordFields value={data.record} /></section><section className="card" aria-label="当前范围计数"><div className="card-heading"><h3>当前目标事实</h3><span className="badge">只读观测</span></div><dl className="parameter-details">{([['已收录目标', data.progress.targets], ['当前已归结目标', data.progress.terminal], ['当前明确失败目标', data.progress.failed], ['当前待确认新单动作', data.progress.pending_submissions], ['采样时间（UTC）', utc(data.observed_at_ms)]] as [string, string][]).map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl><p className="notice">目标计数反映本次采样；新单动作计数属于当前实例，可能包括后来工作。计数变化不改写历史任务结果，也不授予恢复风险的许可。</p></section></>}
    {polling && <p className="muted" role="status">正在等待范围归结，自动查询最多20秒。</p>}{!polling && data?.record.state === 'ACCEPTED' && <p className="notice">范围仍在处理中，可稍后刷新；离开页面不会撤回已受理任务。</p>}
  </>;
}

function ScopeAction({ strategy, session, onExpired, onLock }: { strategy: string; session: Session; onExpired: () => void; onLock: (value: boolean) => void }) {
  const [open, setOpen] = useState(false); const [basis, setBasis] = useState<{ strategy: Strategy; risk: RiskState } | null>(null);
  const [reason, setReason] = useState(''); const [confirmation, setConfirmation] = useState(''); const [confirmed, setConfirmed] = useState(false);
  const [reading, setReading] = useState(false); const [busy, setBusy] = useState(false); const [error, setError] = useState('');
  const [prepared, setPrepared] = useState<EmergencyRequest | null>(null); const write = useRef<AbortController | null>(null); const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; write.current?.abort(); }; }, []);
  useEffect(() => { onLock(busy || prepared !== null); return () => onLock(false); }, [busy, prepared, onLock]);
  useEffect(() => {
    if (!open) return;
    let active = true; const call = new AbortController(); const timer = setTimeout(() => call.abort(), 6000);
    setReading(true); setBasis(null); setError(''); setReason(''); setConfirmation(''); setConfirmed(false);
    Promise.all([getStrategy(strategy, call.signal), getRiskState(strategy, call.signal)]).then(([detail, risk]) => {
      if (active) setBasis({ strategy: detail.strategy, risk });
    }).catch(cause => {
      if (!active) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      setError('暂时无法读取当前策略和风险修订，请取消后重新读取范围。');
    }).finally(() => { clearTimeout(timer); if (active) setReading(false); });
    return () => { active = false; call.abort(); clearTimeout(timer); };
  }, [open, strategy, onExpired]);
  const canWrite = session.strategy_controls_enabled && session.role !== 'viewer';
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!canWrite || busy || write.current || !basis || reading) return;
    let request = prepared;
    if (!request) {
      const note = reason.trim();
      if (!confirmed || confirmation !== strategy || !note || note.includes('\0') || new TextEncoder().encode(note).length > 256 || basis.risk.revision === '9223372036854775807') { setError('请核对策略 ID、确认信息和操作原因。'); return; }
      request = { strategy_id: strategy, key: crypto.randomUUID(), body: { scope: 'strategy', expected_risk_revision: basis.risk.revision, reason: note } }; setPrepared(request);
    }
    const call = new AbortController(); write.current = call; const timer = setTimeout(() => call.abort(), 6000); setBusy(true); setError('');
    try {
      const accepted = await requestEmergency(request, call.signal);
      if (mounted.current) location.assign(pathFor(strategy, accepted.id));
    } catch (cause) {
      if (!mounted.current) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      const code = cause instanceof ApiError ? cause.code : '';
      if (code === 'CONFLICT' || code === 'INVALID_REQUEST' || code === 'NOT_FOUND') { setPrepared(null); setBasis(null); setOpen(false); setError('风险修订或范围已变化，请重新读取并确认。'); }
      else setError(code === 'FORBIDDEN' ? '当前会话无权提交范围撤单；原请求内容保留供核对和重试。' : '尚未确认范围受理结果，请按原请求重试；离开页面不会撤回已受理任务。');
    } finally { clearTimeout(timer); write.current = null; if (mounted.current) setBusy(false); }
  }
  return <section className="card" aria-label="策略范围确认"><div className="card-heading"><h3>策略范围紧急撤单</h3><span className="badge">指定策略 {strategy}</span></div><p className="notice">受理后关闭此策略新增风险，并收录范围内仍有义务的订单。取消确认前保留资金；持仓不会随撤单归零，停止策略与平仓请分别处理。</p>
    {!canWrite ? <p className="muted">当前入口仅支持范围历史和结果查询。</p> : <>
      {!open && <button className="button secondary" onClick={() => setOpen(true)}>读取并确认撤单范围</button>}
      {reading && <p role="status">正在读取当前范围和风险修订…</p>}
      {open && <form className="control-form" onSubmit={submit}>
        {basis && <><dl className="parameter-details"><div><dt>策略 ID</dt><dd>{strategy}</dd></div><div><dt>实例名称</dt><dd>{basis.strategy.instance_key}</dd></div><div><dt>账户</dt><dd>{basis.strategy.account_id}</dd></div><div><dt>交易对 ID</dt><dd>{basis.strategy.instrument_id}</dd></div><div><dt>确认的风险修订</dt><dd>{prepared?.body.expected_risk_revision ?? basis.risk.revision}</dd></div><div><dt>采样时风险开关</dt><dd>{basis.risk.enabled ? '允许新增风险' : '新增风险已关闭'}</dd></div></dl>
          <label htmlFor="emergency-reason">范围撤单原因</label><textarea id="emergency-reason" required maxLength={256} disabled={busy || prepared !== null} value={prepared?.body.reason ?? reason} onChange={event => setReason(event.target.value)} />
          <label htmlFor="emergency-strategy-confirmation">确认策略 ID</label><input id="emergency-strategy-confirmation" required inputMode="numeric" disabled={busy || prepared !== null} value={confirmation} onChange={event => setConfirmation(event.target.value)} />
          <label className="order-checkbox"><input type="checkbox" checked={confirmed} disabled={busy || prepared !== null} onChange={event => setConfirmed(event.target.checked)} />我确认关闭此策略新增风险并撤销范围内订单</label></>}
        <div className="control-actions"><button className="button primary" type="submit" disabled={busy || reading || !basis || !prepared && (!confirmed || confirmation !== strategy || !reason.trim())}>{busy ? '正在提交范围撤单…' : prepared ? '按原请求重试范围撤单' : '确认范围紧急撤单'}</button>{!prepared && <button className="button secondary" type="button" disabled={busy} onClick={() => setOpen(false)}>取消范围确认</button>}</div>
      </form>}
    </>}{error && <p className="error" role="alert">{error}</p>}
  </section>;
}

function History({ strategy, session, onExpired }: { strategy: string; session: Session; onExpired: () => void }) {
  let initialAfter = '0'; let initialLimit = 20;
  try {
    const query = new URLSearchParams(location.search);
    if ([...query.keys()].some(key => !['after_id', 'limit'].includes(key) || query.getAll(key).length !== 1)) throw new Error();
    initialAfter = integer(query.get('after_id') ?? '0', true); const limit = query.get('limit') ?? '20';
    if (!['1', '20', '50', '100'].includes(limit)) throw new Error(); initialLimit = Number(limit);
  } catch { return <p className="error" role="alert">范围历史地址或分页无效。</p>; }
  return <HistoryContent strategy={strategy} session={session} onExpired={onExpired} initialAfter={initialAfter} initialLimit={initialLimit} />;
}
function HistoryContent({ strategy, session, onExpired, initialAfter, initialLimit }: { strategy: string; session: Session; onExpired: () => void; initialAfter: string; initialLimit: number }) {
  const [cursors, setCursors] = useState([initialAfter]); const [limit, setLimit] = useState(initialLimit); const [epoch, setEpoch] = useState(0); const [locked, setLocked] = useState(false);
  const [data, setData] = useState<EmergencyHistory | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(false); const after = cursors.at(-1)!;
  useEffect(() => {
    let active = true; const call = new AbortController(); const timer = setTimeout(() => call.abort(), 6000);
    setData(null); setError(''); setLoading(true);
    history.replaceState(null, '', `${pathFor(strategy)}?after_id=${after}&limit=${limit}`);
    listEmergencies(strategy, after, limit, call.signal).then(result => { if (active) setData(result); }).catch(cause => {
      if (!active) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      setError(readError(cause));
    }).finally(() => { clearTimeout(timer); if (active) setLoading(false); });
    return () => { active = false; call.abort(); clearTimeout(timer); };
  }, [strategy, after, limit, epoch, onExpired]);
  return <><div className="page-heading"><span className="eyebrow">STRATEGY CANCELLATION</span><h2>策略范围撤单</h2><p>核对明确范围、提交原因和持久处理结果。</p></div><ScopeAction strategy={strategy} session={session} onExpired={onExpired} onLock={setLocked} />
    <section className="card" aria-label="范围撤单历史"><div className="card-heading"><h3>范围历史</h3><button className="button secondary compact" disabled={loading || locked} onClick={() => setEpoch(value => value + 1)}>刷新范围历史</button></div><label className="emergency-page-size">每页范围记录<select aria-label="每页范围记录" disabled={locked || loading} value={limit} onChange={event => { setLimit(Number(event.target.value)); setCursors(['0']); }}>{[1, 20, 50, 100].map(value => <option key={value}>{value}</option>)}</select></label>
      {loading && <p role="status">正在读取范围历史…</p>}{error && <p className="error" role="alert">{error}</p>}
      {data && <>{data.items.length === 0 ? <p className="empty-state">此页没有范围撤单记录。</p> : <div className="table-scroll" role="region" aria-label="范围历史列表" tabIndex={0}><table><thead><tr><th>范围 / 受理时间</th><th>存储任务结果</th><th>风险修订</th><th>原因</th><th>结果</th></tr></thead><tbody>{data.items.map(value => <tr key={value.acceptance.id}><td>{value.acceptance.id}<small>{utc(value.acceptance.created_at_ms)}</small></td><td>{emergencyStates[value.state]}<small>任务修订 {value.revision}</small></td><td>{value.acceptance.expected_risk_revision} → {value.acceptance.risk_revision}</td><td>{value.acceptance.reason}</td><td>{locked ? <span>确认处理中</span> : <a className="detail-link" href={pathFor(strategy, value.acceptance.id)} aria-label={`查看范围记录 ${value.acceptance.id}`}>查看结果 ↗</a>}</td></tr>)}</tbody></table></div>}
        <nav className="pagination" aria-label="范围历史分页"><button className="button secondary" disabled={loading || locked || cursors.length === 1} onClick={() => setCursors(cursors.slice(0, -1))}>上一页范围</button><span>当前 {data.items.length} 条</span><button className="button secondary" disabled={loading || locked || data.next_after_id === '0'} onClick={() => setCursors([...cursors, data.next_after_id])}>下一页范围</button></nav><p className="muted">历史采样（UTC）：{utc(data.observed_at_ms)}</p></>}
    </section></>;
}
export function EmergencyWorkspace({ session, onExpired }: { session: Session; onExpired: () => void }) {
  const match = /^\/strategies\/([1-9][0-9]{0,18})\/emergency-cancellations(?:\/([1-9][0-9]{0,18}))?$/.exec(location.pathname);
  try { if (!match) throw new Error(); integer(match[1]); if (match[2]) { integer(match[2]); if (location.search) throw new Error(); } }
  catch { return <p className="error" role="alert">范围撤单地址无效。</p>; }
  const strategy = match![1]; const id = match![2];
  return <div className="strategy-workspace cancellation-workspace emergency-workspace"><a className="back-link" href={id ? pathFor(strategy) : `/strategies/${strategy}`}>{id ? '← 范围撤单历史' : '← 策略详情'}</a>{id ? <EmergencyResult strategy={strategy} id={id} onExpired={onExpired} /> : <History strategy={strategy} session={session} onExpired={onExpired} />}</div>;
}
