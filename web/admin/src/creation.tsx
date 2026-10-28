import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { ApiError } from './api';
import { accountChoices, createStrategy, feedChoices, instrumentChoices } from './creation-api';
import type { CatalogPage, CreationInput, CreationReceipt, FeedChoice } from './creation-api';
import { sources, templates } from './strategy-api';
import type { GridParameters, MakerParameters, Source, Template } from './strategy-api';

type GridDraft = { [K in keyof GridParameters]: string };
type MakerDraft = { [K in Exclude<keyof MakerParameters, 'hedge_enabled' | 'hedge_threshold' | 'hedge_max_wait_ms' | 'max_hedge_backlog'>]: string };
const initialGrid: GridDraft = { lower_price: '98', upper_price: '102', grid_count: '5', total_budget: '50', per_order_notional: '10', inventory_limit: '20', max_market_age_ms: '500' };
const initialMaker: MakerDraft = { quote_levels: '2', level_spacing_bps: '2', base_spread_bps: '10', level_notional: '10', side_budget: '20', inventory_target: '10', inventory_soft_limit: '15', inventory_hard_limit: '20', reprice_bps: '1', min_refresh_interval_ms: '100', max_quote_age_ms: '1000', max_market_age_ms: '500' };
const gridFields: [keyof GridDraft, string][] = [['lower_price', '下界价格'], ['upper_price', '上界价格'], ['grid_count', '网格数量'], ['total_budget', '总预算（报价资产）'], ['per_order_notional', '单笔名义额（报价资产）'], ['inventory_limit', '库存上限（基础资产）'], ['max_market_age_ms', '行情最大年龄（ms）']];
const makerFields: [keyof MakerDraft, string][] = [['quote_levels', '报价档数'], ['level_spacing_bps', '档距（BPS）'], ['base_spread_bps', '基础价差（BPS）'], ['level_notional', '单档名义额（报价资产）'], ['side_budget', '单侧预算（报价资产）'], ['inventory_target', '目标库存（基础资产）'], ['inventory_soft_limit', '库存软限（基础资产）'], ['inventory_hard_limit', '库存硬限（基础资产）'], ['reprice_bps', '重报阈值（BPS）'], ['min_refresh_interval_ms', '最小刷新间隔（ms）'], ['max_quote_age_ms', '挂单最大年龄（ms）'], ['max_market_age_ms', '行情最大年龄（ms）']];
const identifier = /^[A-Za-z0-9_.:-]{1,64}$/;
const decimalInput = /^(0|[1-9][0-9]{0,19})(\.[0-9]{1,18})?$/;

function useChoices<T extends { id: string }>(load: (cursor: string, signal: AbortSignal) => Promise<CatalogPage<T>>, onExpired: () => void) {
  const [items, setItems] = useState<T[]>([]); const [cursor, setCursor] = useState('0'); const [next, setNext] = useState('0');
  const [loading, setLoading] = useState(true); const [error, setError] = useState(false); const [epoch, setEpoch] = useState(0);
  useEffect(() => {
    let active = true; const controller = new AbortController(); const timer = setTimeout(() => controller.abort(), 6000);
    setLoading(true); setError(false);
    load(cursor, controller.signal).then(page => { if (active) { setItems(old => cursor === '0' ? page.items : [...new Map([...old, ...page.items].map(item => [item.id, item])).values()]); setNext(page.next); } }).catch(cause => {
      if (!active) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') onExpired(); else setError(true);
    }).finally(() => { clearTimeout(timer); if (active) setLoading(false); });
    return () => { active = false; controller.abort(); clearTimeout(timer); };
  }, [load, cursor, epoch, onExpired]);
  return { items, loading, error, more: next !== '0' ? () => setCursor(next) : null, retry: () => setEpoch(e => e + 1) };
}

export function CreationForm({ onExpired, onCreated, onClose }: { onExpired: () => void; onCreated: () => void; onClose: () => void }) {
  const accounts = useChoices(accountChoices, onExpired); const instruments = useChoices(instrumentChoices, onExpired);
  const [account, setAccount] = useState(''); const [instrument, setInstrument] = useState(''); const [mode, setMode] = useState<'LIVE' | 'REPLAY'>('REPLAY');
  const [feeds, setFeeds] = useState<FeedChoice[]>([]); const [source, setSource] = useState<Source | ''>(''); const [feedLoading, setFeedLoading] = useState(false); const [feedError, setFeedError] = useState(false); const [feedEpoch, setFeedEpoch] = useState(0);
  const [template, setTemplate] = useState<Template>('grid'); const [name, setName] = useState(''); const [code, setCode] = useState(''); const [reason, setReason] = useState('');
  const [grid, setGrid] = useState<GridDraft>(initialGrid); const [maker, setMaker] = useState<MakerDraft>(initialMaker);
  const [prepared, setPrepared] = useState<{ body: CreationInput; key: string } | null>(null); const [receipt, setReceipt] = useState<CreationReceipt | null>(null);
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const controller = useRef<AbortController | null>(null); const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; controller.current?.abort(); }; }, []);
  useEffect(() => {
    setSource(''); setFeeds([]); setFeedError(false); setFeedLoading(false); if (!instrument) return;
    let active = true; const request = new AbortController(); const timer = setTimeout(() => request.abort(), 6000); setFeedLoading(true);
    feedChoices(instrument, mode, request.signal).then(value => { if (active) setFeeds(value); }).catch(cause => {
      if (!active) return; if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') onExpired(); else setFeedError(true);
    }).finally(() => { clearTimeout(timer); if (active) setFeedLoading(false); });
    return () => { active = false; request.abort(); clearTimeout(timer); };
  }, [instrument, mode, feedEpoch, onExpired]);
  const selected = instruments.items.find(i => i.id === instrument);
  const unavailable = accounts.loading || instruments.loading || accounts.error || instruments.error || feedLoading || feedError;
  const frozen = busy || prepared !== null;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (controller.current || busy) return;
    let request = prepared;
    if (!request) {
      const note = reason.trim(); const fields = template === 'grid' ? gridFields : makerFields; const draft = template === 'grid' ? grid : maker;
      const validFields = fields.every(([key]) => { const value = draft[key as keyof typeof draft] as string; return key === 'grid_count' || key === 'quote_levels' ? /^[1-9][0-9]{0,2}$/.test(value) && Number(value) <= (key === 'grid_count' ? 200 : 20) : key.endsWith('_ms') ? /^[1-9][0-9]{0,6}$/.test(value) && BigInt(value) <= 3600000n : decimalInput.test(value); });
      if (!identifier.test(name) || !identifier.test(code) || !note || new TextEncoder().encode(note).length > 256 || note.includes('\0') || !source || !feeds.some(f => f.source === source) || !accounts.items.some(a => a.account_id === account && a.enabled) || !selected || selected.status !== 'TRADING' || !validFields) { setError('请检查账户、来源、实例标识和参数格式；原因应为 1–256 字节。'); return; }
      const parameters: CreationInput['parameters'] = template === 'grid' ? { grid: { ...grid, grid_count: Number(grid.grid_count) } } : { market_maker: { ...maker, quote_levels: Number(maker.quote_levels), hedge_enabled: false, hedge_threshold: '0', hedge_max_wait_ms: '0', max_hedge_backlog: '0' } };
      request = { key: crypto.randomUUID(), body: { instance_key: name, account_id: account, instrument_id: instrument, code_version: code, market_data_source: source, market_mode: mode, parameters, reason: note } };
      setPrepared(request);
    }
    const call = new AbortController(); controller.current = call; const timer = setTimeout(() => call.abort(), 6000); setBusy(true); setError('');
    try { const result = await createStrategy(request.body, request.key, call.signal); if (mounted.current) { setReceipt(result); setPrepared(null); onCreated(); } }
    catch (cause) {
      if (!mounted.current) return;
      if (cause instanceof ApiError && cause.code === 'UNAUTHENTICATED') { onExpired(); return; }
      if (cause instanceof ApiError && (cause.code === 'INVALID_REQUEST' || cause.code === 'CONFLICT')) { setPrepared(null); setError(cause.code === 'CONFLICT' ? '实例标识或请求内容冲突，请核对已有实例后修改。' : '参数或账户依赖未满足，请检查配置和开通状态。'); }
      else setError(cause instanceof ApiError && cause.code === 'FORBIDDEN' ? '当前创建未获授权；重试会保留原请求。' : '尚未确认创建结果，请按原请求重试；离开页面不会撤回请求。');
    } finally { clearTimeout(timer); controller.current = null; if (mounted.current) setBusy(false); }
  }
  return <section className="card creation-card" aria-label="创建策略"><div className="card-heading"><h3>创建策略</h3><button className="button secondary compact" disabled={busy} onClick={onClose}>关闭创建</button></div>
    {receipt ? <><p className="notice" role="status">策略已创建。尚未请求运行，请进入详情查看当前状态并单独启动。</p><p>创建收据 {receipt.id} · 初始配置 v1 · {sources[receipt.market_data_source]}</p><a className="detail-link" href={`/strategies/${receipt.strategy_id}`}>查看新策略 ↗</a></> : <form onSubmit={submit}>
      <p className="notice">仅保存初始配置，不会自动启动或开通资金。初始参数为示例，请按交易对检查；小数按字符串提交。</p>
      {(accounts.error || instruments.error) && <div className="error" role="alert">目录读取失败。<button type="button" className="button secondary compact" onClick={() => { accounts.retry(); instruments.retry(); }}>重试目录</button></div>}
      {!accounts.loading && !accounts.error && accounts.items.length === 0 && <p className="notice">暂无已开通账户。</p>}
      {!instruments.loading && !instruments.error && instruments.items.length === 0 && <p className="notice">暂无可选交易对。</p>}
      <fieldset disabled={frozen}><div className="creation-fields">
        <label>实例标识<input required value={name} maxLength={64} onChange={e => setName(e.target.value)} /></label>
        <label>代码版本<input required value={code} maxLength={64} onChange={e => setCode(e.target.value)} /></label>
        <label>账户<select aria-label="账户" required value={account} onChange={e => setAccount(e.target.value)}><option value="">请选择账户</option>{accounts.items.map(a => <option key={a.id} value={a.account_id} disabled={!a.enabled}>{a.account_id}{a.enabled ? '' : '（已禁用）'}</option>)}</select></label>
        <label>交易对<select aria-label="交易对" required value={instrument} onChange={e => { setSource(''); setInstrument(e.target.value); }}><option value="">请选择交易对</option>{instruments.items.map(i => <option key={i.id} value={i.id} disabled={i.status !== 'TRADING'}>{i.symbol}{i.status === 'TRADING' ? '' : '（已暂停）'}</option>)}</select></label>
        <label>行情模式<select aria-label="行情模式" value={mode} onChange={e => { setSource(''); setMode(e.target.value as 'LIVE' | 'REPLAY'); }}><option value="REPLAY">回放行情</option><option value="LIVE">实时行情</option></select></label>
        <label>行情来源<select aria-label="行情来源" required value={source} disabled={feedLoading || feedError || !instrument} onChange={e => setSource(e.target.value as Source | '')}><option value="">请选择已绑定来源</option>{feeds.map(f => <option key={f.id} value={f.source}>{sources[f.source]}</option>)}</select></label>
        <label>策略模板<select aria-label="策略模板" value={template} onChange={e => setTemplate(e.target.value as Template)}>{Object.entries(templates).map(([key, value]) => <option key={key} value={key}>{value}</option>)}</select></label>
      </div>
      {accounts.more && <button type="button" className="button secondary compact" disabled={accounts.loading} onClick={accounts.more}>加载更多账户</button>}{instruments.more && <button type="button" className="button secondary compact" disabled={instruments.loading} onClick={instruments.more}>加载更多交易对</button>}
      {selected && <p className="notice">价格步长 {selected.price_tick} · 数量步长 {selected.quantity_step} · 最小名义额 {selected.min_notional}</p>}
      {feedLoading && <p role="status">正在读取所选模式的来源…</p>}{instrument && !feedLoading && !feedError && feeds.length === 0 && <p className="notice">当前模式没有已绑定来源，请检查行情配置。</p>}
      {feedError && <p className="error" role="alert">来源读取失败。<button type="button" className="button secondary compact" onClick={() => setFeedEpoch(e => e + 1)}>重试来源</button></p>}
      <h4>策略参数</h4>{template === 'market_maker' && <p className="muted">对冲：关闭</p>}<div className="creation-fields">{template === 'grid' ? gridFields.map(([key, label]) => <label key={key}>{label}<input required inputMode="decimal" value={grid[key]} maxLength={40} onChange={e => setGrid({ ...grid, [key]: e.target.value })} /></label>) : makerFields.map(([key, label]) => <label key={key}>{label}<input required inputMode="decimal" value={maker[key]} maxLength={40} onChange={e => setMaker({ ...maker, [key]: e.target.value })} /></label>)}</div>
      <label>创建原因<textarea required value={reason} maxLength={256} onChange={e => setReason(e.target.value)} /></label></fieldset>
      {error && <p className="error" role="alert">{error}</p>}<button type="submit" className="button primary" disabled={busy || !prepared && unavailable}>{busy ? '正在创建…' : prepared ? '按原请求重试创建' : '确认创建'}</button>
    </form>}
  </section>;
}
