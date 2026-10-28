import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import type { Session } from './api';
import { actions, controlStrategy, getOperation, operationStates } from './operations-api';
import type { Action, ControlRequest, Operation } from './operations-api';
import { sources, states } from './strategy-api';
import type { Strategy } from './strategy-api';

function useOperation(id: string, initial: Operation | null, onExpired: () => void) {
  const [data, setData] = useState(initial);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(!initial);
  const [polling, setPolling] = useState(initial?.state === 'ACCEPTED');
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let active = true;
    let controller: AbortController | null = null;
    let next: ReturnType<typeof setTimeout> | undefined;
    const end = Date.now() + 20000;
    setError(''); setPolling(true);
    async function read() {
      controller = new AbortController();
      const timeout = setTimeout(() => controller?.abort(), 6000);
      try {
        const result = await getOperation(id, controller.signal);
        if (!active) return;
        setData(result); setLoading(false);
        if (result.state === 'ACCEPTED' && Date.now() < end) next = setTimeout(read, 1000);
        else setPolling(false);
      } catch (cause) {
        if (!active) return;
        if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
        setLoading(false); setPolling(false);
        const code = cause instanceof ApiError ? cause.code : '';
        setError(code === 'NOT_FOUND' ? '找不到此操作记录。' : code === 'FORBIDDEN' ? '当前账户无权读取操作结果。' : '暂时无法确认操作结果，请查询最新记录。');
      } finally { clearTimeout(timeout); }
    }
    void read();
    return () => { active = false; controller?.abort(); clearTimeout(next); };
  }, [id, onExpired, revision]);
  return { data, error, loading, polling, refresh: () => setRevision(value => value + 1) };
}

function OperationRecord({ operation }: { operation: Operation }) {
  return <><div className="card-heading"><h3>操作结果</h3><span className={`state-badge ${operation.state === 'SUCCEEDED' ? 'running' : ''}`}>{operationStates[operation.state]}</span></div>
    {operation.state === 'ACCEPTED' && <p className="notice">请求已持久受理，正在等待实际状态确认。</p>}
    {operation.state === 'SUPERSEDED' && <p className="notice">此请求目标已被后续操作替换，保留原记录供核对。</p>}
    <dl className="parameter-details"><div><dt>操作 ID</dt><dd>{operation.id}</dd></div><div><dt>操作</dt><dd>{actions[operation.action].title}</dd></div><div><dt>策略 ID</dt><dd>{operation.strategy_id}</dd></div><div><dt>请求目标</dt><dd>{states[operation.target_state]}</dd></div><div><dt>提交原因</dt><dd>{operation.reason}</dd></div><div><dt>请求配置</dt><dd>v{operation.config_version} · {sources[operation.market_data_source]}</dd></div><div><dt>请求修订</dt><dd>{operation.expected_revision} → {operation.effect_revision}</dd></div>
      {operation.observation && <><div><dt>已观察实际状态</dt><dd>{states[operation.observation.state]}</dd></div><div><dt>已观察修订</dt><dd>{operation.observation.strategy_revision}</dd></div></>}
    </dl></>;
}

function OperationReceipt({ initial, onExpired, onTerminal }: { initial: Operation; onExpired: () => void; onTerminal: () => void }) {
  const result = useOperation(initial.id, initial, onExpired);
  const notified = useRef(false);
  const terminalCallback = useRef(onTerminal);
  useEffect(() => { terminalCallback.current = onTerminal; }, [onTerminal]);
  useEffect(() => {
    if (result.data && result.data.state !== 'ACCEPTED' && !notified.current) { notified.current = true; terminalCallback.current(); }
  }, [result.data]);
  return <section className="operation-receipt" aria-label="最新操作"><OperationRecord operation={result.data ?? initial} />
    {result.error && <p className="error" role="alert">{result.error}</p>}
    <div className="operation-links"><a className="detail-link" href={`/operations/${initial.id}`}>查看操作结果 ↗</a><button className="button secondary compact" onClick={result.refresh} disabled={result.polling}>查询最新结果</button></div>
    {result.polling && <p className="muted" role="status">正在确认结果…</p>}
    {!result.polling && result.data?.state === 'ACCEPTED' && <p className="notice">操作仍在等待确认，可稍后查询最新结果。</p>}
  </section>;
}

function allowed(strategy: Strategy, action: Action): boolean {
  if (action === 'strategy.start') return strategy.state === 'CREATED' || strategy.state === 'STOPPED';
  if (action === 'strategy.resume') return strategy.state === 'PAUSED';
  if (action === 'strategy.pause') return strategy.state === 'RUNNING' || strategy.state === 'STARTING' || strategy.state === 'RECOVERING';
  return true;
}

export function StrategyControls({ strategy, loading, session, onExpired, onRefresh }: { strategy: Strategy | undefined; loading: boolean; session: Session; onExpired: () => void; onRefresh: () => void }) {
  const [snapshot, setSnapshot] = useState<Strategy | null>(null);
  const [action, setAction] = useState<Action | null>(null);
  const [reason, setReason] = useState('');
  const [prepared, setPrepared] = useState<ControlRequest | null>(null);
  const [receipt, setReceipt] = useState<Operation | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const controller = useRef<AbortController | null>(null);
  const mounted = useRef(true);
  useEffect(() => { if (strategy) setSnapshot(strategy); }, [strategy]);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort(); }; }, []);
  const current = strategy ?? snapshot;
  if (!session.strategy_controls_enabled || !current) return null;
  const canWrite = session.role === 'operator' || session.role === 'admin';
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy || controller.current || !current || !action) return;
    const normalized = reason.trim();
    if (!prepared && (new TextEncoder().encode(normalized).length === 0 || new TextEncoder().encode(normalized).length > 256 || normalized.includes('\0'))) { setError('操作原因应为 1–256 字节。'); return; }
    const request = prepared ?? { strategy_id: current.id, action, expected_revision: current.revision, reason: normalized, key: crypto.randomUUID() };
    if (!prepared) setReceipt(null);
    setPrepared(request); setBusy(true); setError('');
    const call = new AbortController(); controller.current = call;
    const timer = setTimeout(() => call.abort(), 6000);
    try {
      const operation = await controlStrategy(request, call.signal);
      if (!mounted.current) return;
      setReceipt(operation); setPrepared(null); setAction(null); setReason(''); onRefresh();
    } catch (cause) {
      if (!mounted.current) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      if (cause instanceof ApiError && (cause.code === 'CONFLICT' || cause.code === 'INVALID_REQUEST')) {
        setPrepared(null); setAction(null); onRefresh();
        setError(cause.code === 'CONFLICT' ? '实例或请求版本已变化，请刷新状态后重新选择操作。' : '操作参数无效，请检查后重新提交。');
      } else if (cause instanceof ApiError && cause.code === 'FORBIDDEN') {
        setError('此操作未获授权，请检查当前会话与权限；重试会保留原请求内容。');
      } else setError('尚未确认此请求的受理结果。请按原请求重试；离开页面不会撤回已发送请求。');
    } finally { clearTimeout(timer); if (mounted.current) setBusy(false); controller.current = null; }
  }
  return <section className="card control-card" aria-label="运行控制"><div className="card-heading"><h3>运行控制</h3><span className="badge">{canWrite ? '受审计操作' : '只读访问'}</span></div>
    {canWrite ? <><p className="notice">提交时验证当前修订与权限，完成状态以实际运行记录为准。</p>
      {!action && <div className="control-actions">{(Object.keys(actions) as Action[]).map(value => <button className={`button secondary ${value === 'strategy.stop' ? 'stop-action' : ''}`} key={value} disabled={busy || loading || !allowed(current, value) || current.state === actions[value].target && current.desired_state === actions[value].target} onClick={() => { setAction(value); setReason(''); setError(''); }}>{actions[value].title}</button>)}</div>}
      {action && <form className="control-form" onSubmit={submit}><h4>确认{actions[action].title}</h4><p>{actions[action].description}</p><p className="muted">当前实例修订 {prepared?.expected_revision ?? current.revision}</p><label htmlFor="control-reason">操作原因</label><textarea id="control-reason" required maxLength={256} value={prepared?.reason ?? reason} onChange={event => setReason(event.target.value)} disabled={busy || prepared !== null} />
        <div className="control-actions"><button className="button primary" type="submit" disabled={busy || loading}>{busy ? '正在提交…' : prepared ? '按原请求重试' : `确认${actions[action].title}`}</button>{!prepared && <button className="button secondary" type="button" disabled={busy} onClick={() => { setAction(null); setError(''); }}>取消</button>}</div></form>}
    </> : <p className="notice">当前账户为只读用户，可查看状态与操作结果。</p>}
    {error && <p className="error" role="alert">{error}</p>}
    {receipt && <OperationReceipt key={receipt.id} initial={receipt} onExpired={onExpired} onTerminal={onRefresh} />}
  </section>;
}

export function OperationWorkspace({ onExpired }: { onExpired: () => void }) {
  const id = /^\/operations\/([1-9][0-9]{0,18})$/.exec(location.pathname)?.[1] ?? '';
  const result = useOperation(id, null, onExpired);
  return <div className="strategy-workspace"><a className="back-link" href={result.data ? `/strategies/${result.data.strategy_id}` : '/strategies'}>← 策略详情</a><div className="page-heading"><span className="eyebrow">OPERATION RESULT</span><h2>操作结果</h2><p>查看请求的持久结果与实际状态证明。</p></div>
    {result.loading && <div className="card loading" role="status"><span className="spinner" />正在读取操作结果…</div>}
    {result.data && <section className="card" aria-label="操作记录"><OperationRecord operation={result.data} /></section>}
    {result.error && <p className="error" role="alert">{result.error}</p>}
    <button className="button secondary" disabled={result.polling} onClick={result.refresh}>{result.polling ? '正在确认结果…' : '查询最新结果'}</button>
  </div>;
}
