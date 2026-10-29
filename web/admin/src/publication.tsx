import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import { feedChoices } from './creation-api';
import type { FeedChoice } from './creation-api';
import { gridFields, makerFields, parameterDraft, parameterInput, selectableSource, validDraft } from './parameter-draft';
import { publishConfiguration } from './publication-api';
import type { PublicationInput, PublicationReceipt } from './publication-api';
import { sources, states, templates } from './strategy-api';
import type { Source, StrategyDetail } from './strategy-api';

export function PublicationForm({ initial, current, loading, onExpired, onRefresh, onClose }: { initial: StrategyDetail; current: StrategyDetail | null; loading: boolean; onExpired: () => void; onRefresh: () => void; onClose: () => void }) {
  const [basis, setBasis] = useState(initial);
  const parameters = basis.requested_config.parameters;
  const [draft, setDraft] = useState(() => parameterDraft(initial.requested_config.parameters!));
  const [source, setSource] = useState<Source | ''>(initial.strategy.requested_market_data_source);
  const [feeds, setFeeds] = useState<FeedChoice[]>([]); const [feedLoading, setFeedLoading] = useState(true); const [feedError, setFeedError] = useState(false); const [feedEpoch, setFeedEpoch] = useState(0);
  const [reason, setReason] = useState(''); const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  const [prepared, setPrepared] = useState<{ body: PublicationInput; key: string } | null>(null); const [receipt, setReceipt] = useState<PublicationReceipt | null>(null);
  const controller = useRef<AbortController | null>(null); const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort(); }; }, []);
  useEffect(() => {
    let active = true; const call = new AbortController(); const timer = setTimeout(() => call.abort(), 6000); setFeedLoading(true); setFeedError(false); setFeeds([]);
    feedChoices(basis.strategy.instrument_id, basis.strategy.market_mode, call.signal).then(value => { if (active) setFeeds(value); }).catch(cause => {
      if (!active) return; if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') onExpired(); else setFeedError(true);
    }).finally(() => { clearTimeout(timer); if (active) setFeedLoading(false); });
    return () => { active = false; call.abort(); clearTimeout(timer); };
  }, [basis.strategy.instrument_id, basis.strategy.market_mode, feedEpoch, onExpired]);
  const frozen = busy || prepared !== null;
  const stale = current !== null && (current.strategy.revision !== basis.strategy.revision || current.strategy.requested_config_version !== basis.strategy.requested_config_version);
  function resetBasis() {
    if (!current?.requested_config.parameters || frozen) return;
    setBasis(current); setDraft(parameterDraft(current.requested_config.parameters)); setSource(current.strategy.requested_market_data_source); setError(''); setFeedEpoch(value => value + 1);
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (controller.current || busy) return;
    let request = prepared;
    if (!request) {
      const note = reason.trim();
      if (loading || !current || stale || feedLoading || feedError || !selectableSource(source, feeds) || !validDraft(basis.strategy.template, draft.grid, draft.maker) || !note || new TextEncoder().encode(note).length > 256 || note.includes('\0') || parameters?.kind === 'market_maker' && parameters.values.hedge_enabled) { setError('请核对当前请求版本、已绑定来源、参数格式和发布原因。'); return; }
      request = { key: crypto.randomUUID(), body: { expected_revision: basis.strategy.revision, expected_config_version: basis.strategy.requested_config_version, market_data_source: source, parameters: parameterInput(basis.strategy.template, draft.grid, draft.maker), reason: note } }; setPrepared(request);
    }
    const call = new AbortController(); controller.current = call; const timer = setTimeout(() => call.abort(), 6000); setBusy(true); setError('');
    try { const value = await publishConfiguration(basis.strategy.id, request.body, request.key, call.signal); if (mounted.current) { setReceipt(value); setPrepared(null); onRefresh(); } }
    catch (cause) {
      if (!mounted.current) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      if (cause instanceof ApiError && (cause.code === 'CONFLICT' || cause.code === 'INVALID_REQUEST')) { setPrepared(null); setError(cause.code === 'CONFLICT' ? '配置或修订已变化，请刷新并重新载入当前请求后确认。' : '参数或开通条件未满足，请检查配置。'); if (cause.code === 'CONFLICT') onRefresh(); }
      else setError(cause instanceof ApiError && cause.code === 'FORBIDDEN' ? '本次发布未获授权；按原请求重试会保留全部内容。' : '尚未确认发布结果，请按原请求重试；离开页面不会撤回请求。');
    } finally { clearTimeout(timer); controller.current = null; if (mounted.current) setBusy(false); }
  }
  return <section className="card creation-card" aria-label="发布参数与换源"><div className="card-heading"><h3>发布参数与换源</h3><button className="button secondary compact" disabled={busy} onClick={onClose}>关闭发布</button></div>
    <p className="notice">保存完整新版本，保持现有运行目标。运行中的实例会清理旧单并等待所选来源就绪；请查看生效配置确认进度。</p>
    <p className="muted publication-summary">模板 · {templates[basis.strategy.template]}　账户 · {basis.strategy.account_id}　交易对 · {basis.strategy.instrument_id}</p>
    <p className="muted">编辑基线：修订 {basis.strategy.revision} · 请求 v{basis.strategy.requested_config_version} / {sources[basis.strategy.requested_market_data_source]}</p>
    {current && <p className="muted">最近读取：请求 v{current.strategy.requested_config_version} / {sources[current.strategy.requested_market_data_source]} · 生效 {current.strategy.active_config_version ? `v${current.strategy.active_config_version} / ${sources[current.strategy.active_market_data_source!]}` : '尚未'} · 目标 {states[current.strategy.desired_state]}</p>}
    {receipt ? <><p className="notice" role="status">已记录请求版本 v{receipt.config_version}。请分别核对请求配置与生效配置，发布收据不代表已完成切换。</p><p>发布收据 {receipt.id} · {sources[receipt.market_data_source]} · 修订 {receipt.previous_revision} → {receipt.revision}</p>{current && BigInt(current.strategy.requested_config_version) > BigInt(receipt.config_version) && <p className="notice">当前请求已有更新版本，本收据保留原提交记录。</p>}</> : <form onSubmit={submit}>
      {stale && !prepared && <p className="notice" role="status">实例已更新，编辑基线需要重新载入。</p>}
      <button type="button" className="button secondary compact" disabled={frozen || loading || !current?.requested_config.parameters} onClick={resetBasis}>载入当前请求</button>
      <fieldset disabled={frozen}><label>新版本行情来源<select aria-label="新版本行情来源" required value={source} disabled={feedLoading || feedError} onChange={event => setSource(event.target.value as Source | '')}><option value="">请选择已绑定来源</option>{feeds.map(feed => <option key={feed.id} value={feed.source}>{sources[feed.source]}</option>)}</select></label>
        {feedLoading && <p role="status">正在读取所选模式的来源…</p>}{!feedLoading && !feedError && feeds.length === 0 && <p className="notice">当前模式没有已绑定来源，请检查行情配置。</p>}{feedError && <p className="error" role="alert">来源读取失败。<button type="button" className="button secondary compact" onClick={() => setFeedEpoch(value => value + 1)}>重试来源</button></p>}
        <h4>新版本参数</h4>{basis.strategy.template === 'market_maker' && <p className="muted">对冲：关闭</p>}<div className="creation-fields">{basis.strategy.template === 'grid' ? gridFields.map(([key, label]) => <label key={key}>{label}<input required inputMode="decimal" maxLength={40} value={draft.grid[key]} onChange={event => setDraft({ ...draft, grid: { ...draft.grid, [key]: event.target.value } })} /></label>) : makerFields.map(([key, label]) => <label key={key}>{label}<input required inputMode="decimal" maxLength={40} value={draft.maker[key]} onChange={event => setDraft({ ...draft, maker: { ...draft.maker, [key]: event.target.value } })} /></label>)}</div>
        <label>发布原因<textarea aria-label="发布原因" required value={reason} maxLength={256} onChange={event => setReason(event.target.value)} /></label>
      </fieldset>{error && <p className="error" role="alert">{error}</p>}<button type="submit" className="button primary" disabled={busy || !prepared && (loading || !current || stale || feedLoading || feedError)}>{busy ? '正在发布…' : prepared ? '按原请求重试发布' : '确认发布新版本'}</button>
    </form>}
  </section>;
}
