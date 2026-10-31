import { StrictMode, useCallback, useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { createRoot } from 'react-dom/client';
import { ApiError, currentSession, errorText, login, logout } from './api';
import type { Role, Session } from './api';
import { StrategyWorkspace } from './strategies';
import { OperationWorkspace } from './operations';
import { CancellationWorkspace } from "./cancellations";
import { AuditWorkspace } from './audits';
import { MarketWorkspace } from './markets';
import { TradeWorkspace } from './trades';
import { PortfolioWorkspace } from './portfolios';
import { RiskEventWorkspace } from './risk-events';
import { OverviewWorkspace } from './overview';
import './style.css';

const roles: Record<Role, string> = { viewer: '只读用户', operator: '操作员', admin: '管理员' };

function Mark() {
  return <span className="mark" aria-hidden="true"><svg viewBox="0 0 32 32" fill="none"><path d="m7 4 4 8 5 3 5-3 4-8M4 10l7 2m17-2-7 2M11 12l1 12 4 5 4-5 1-12M12 20l4-2 4 2" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg></span>;
}

function expiry(value: string): string {
  // This conversion is limited to integer UTC milliseconds, never financial values.
  const timestamp = Number(value);
  if (!Number.isSafeInteger(timestamp)) return '—';
  return new Date(timestamp).toLocaleString('zh-CN', { hour12: false });
}

function App() {
  const [session, setSession] = useState<Session | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  const strategyView = session?.strategy_queries_enabled && location.pathname.startsWith('/strategies');
  const cancellationView = strategyView && location.pathname.includes("/order-cancellations/");
  const tradeView = strategyView && location.pathname.includes('/trades');
  const operationView = session?.strategy_controls_enabled && location.pathname.startsWith('/operations');
  const auditView = session?.strategy_queries_enabled && location.pathname.startsWith('/audits');
  const marketView = session?.strategy_queries_enabled && location.pathname.startsWith('/markets');
  const portfolioView = session?.strategy_queries_enabled && location.pathname.startsWith('/portfolios');
  const riskEventView = session?.strategy_queries_enabled && location.pathname.startsWith('/risk-events');
  const overviewView = session?.strategy_queries_enabled && location.pathname === '/overview';
  const managementView = strategyView || operationView || auditView || marketView || portfolioView || riskEventView || overviewView;
  const sessionExpired = useCallback(() => {
    setSession(null); setPassword(''); setError('会话已失效，请重新登录。');
    history.replaceState(null, '', '/login');
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    let active = true;
    setLoading(true);
    setError('');
    currentSession(controller.signal).then(value => {
      if (active) setSession(value);
    }).catch(cause => {
      if (active && !(cause instanceof ApiError && cause.code === 'UNAUTHENTICATED')) setError(errorText(cause));
    }).finally(() => { if (active) setLoading(false); });
    return () => { active = false; controller.abort(); };
  }, [retry]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) return;
    const bytes = new TextEncoder().encode(password).length;
    if (bytes < 12 || bytes > 72) { setError('密码长度应为 12–72 字节。'); return; }
    setBusy(true);
    setError('');
    try {
      const value = await login(username, password);
      setPassword('');
      setSession(value);
      if (!(value.strategy_queries_enabled && (location.pathname.startsWith('/strategies') || location.pathname.startsWith('/audits') || location.pathname.startsWith('/markets') || location.pathname.startsWith('/portfolios') || location.pathname.startsWith('/risk-events') || location.pathname === '/overview')) && !(value.strategy_controls_enabled && location.pathname.startsWith('/operations'))) history.replaceState(null, '', '/session');
    } catch (cause) { setError(errorText(cause)); }
    finally { setBusy(false); }
  }

  async function signOut() {
    setBusy(true);
    setError('');
    try {
      await logout();
      setSession(null);
      setPassword('');
      history.replaceState(null, '', '/login');
    } catch (cause) { setError(errorText(cause)); }
    finally { setBusy(false); }
  }

  return <div className={`shell${managementView ? ' workbench' : ''}`}>
    <aside className="brand-panel">
      <a className="brand" href="/"><Mark /><span>MUSKDEER<small>管理工作台</small></span></a>
      <div className="brand-copy"><span className="eyebrow">CONTROL WITH CLARITY</span><h1>身份明确，<br />操作有据。</h1><p>在统一的管理入口中，<br />用明确的身份与权限开始工作。</p></div>
      <div className="brand-footer"><span className="status-dot" /> 专属账户 · 独立会话</div>
    </aside>
    <main>
      <header className={`topbar${session?.strategy_queries_enabled ? ' console-topbar' : ''}`}>{session?.strategy_queries_enabled ? <nav className="console-nav" aria-label="控制台导航"><a href="/session" aria-current={!managementView ? 'page' : undefined}>账户与访问</a><a href="/overview" aria-current={overviewView ? 'page' : undefined}>运行概览</a><a href="/strategies" aria-current={strategyView || operationView ? 'page' : undefined}>策略管理</a><a href="/markets" aria-current={marketView ? 'page' : undefined}>行情监控</a><a href="/portfolios" aria-current={portfolioView ? 'page' : undefined}>账户持仓</a><a href="/risk-events" aria-current={riskEventView ? 'page' : undefined}>风险历史</a><a href="/audits" aria-current={auditView ? 'page' : undefined}>审计记录</a></nav> : <span>账户与访问</span>}<span className="workspace-tag">{managementView && session ? `${session.username} · ${roles[session.role]}` : 'MUSKDEER / CONSOLE'}</span></header>
      <section className="content" aria-busy={loading || busy}>
        {loading ? <div className="card loading" role="status"><span className="spinner" />正在确认登录状态…</div> : session ? overviewView ? <OverviewWorkspace onExpired={sessionExpired} /> : riskEventView ? <RiskEventWorkspace onExpired={sessionExpired} /> : portfolioView ? <PortfolioWorkspace onExpired={sessionExpired} /> : marketView ? <MarketWorkspace onExpired={sessionExpired} /> : auditView ? <AuditWorkspace onExpired={sessionExpired} /> : operationView ? <OperationWorkspace onExpired={sessionExpired} /> : cancellationView ? <CancellationWorkspace onExpired={sessionExpired} /> : tradeView ? <TradeWorkspace onExpired={sessionExpired} /> : strategyView ? <StrategyWorkspace onExpired={sessionExpired} session={session} /> :
          <div className="session-layout"><div className="page-heading"><span className="eyebrow">YOUR WORKSPACE</span><h2>你好，{session.username}</h2><p>你已安全登录 Muskdeer 管理工作台。</p></div>
            <div className="card"><div className="card-heading"><h3>当前会话</h3><span className="badge"><span className="status-dot" /> 已登录</span></div>
              <dl className="session-details"><div><dt>账户</dt><dd>{session.username}</dd></div><div><dt>角色</dt><dd>{roles[session.role]}</dd></div><div><dt>登录有效期至</dt><dd>{expiry(session.expires_at_ms)}</dd></div></dl>
              <p className="notice">操作权限由服务端独立校验，退出后此会话将立即失效。</p>
              {session.strategy_queries_enabled && <a className="button primary" href="/strategies">查看策略实例<span aria-hidden="true">→</span></a>}
              {error && <div className="error" role="alert">{error}</div>}
              <button className="button secondary" type="button" disabled={busy} onClick={signOut}>{busy ? '正在退出…' : '退出当前会话'}<span aria-hidden="true">↗</span></button>
            </div>
          </div> : <div className="login-layout"><div className="page-heading"><span className="eyebrow">WELCOME BACK</span><h2>登录工作台</h2><p>使用你的管理账户继续。</p></div>
            <form className="card login-form" onSubmit={submit}>
              <label htmlFor="username">用户名</label><input id="username" name="username" autoComplete="username" required minLength={3} maxLength={64} value={username} onChange={event => setUsername(event.target.value)} placeholder="请输入用户名" disabled={busy} />
              <label htmlFor="password">密码</label><input id="password" name="password" type="password" autoComplete="current-password" required maxLength={72} value={password} onChange={event => setPassword(event.target.value)} placeholder="请输入密码" disabled={busy} />
              {error && <div className="error" role="alert">{error}</div>}
              <button className="button primary" type="submit" disabled={busy}>{busy ? '正在验证身份…' : '登录'}<span aria-hidden="true">→</span></button>
              <div className="form-footer"><span className="lock-icon" aria-hidden="true">◇</span> 账户由平台管理员开通，无公开注册入口。</div>
            </form>
            {error && <button className="text-button" type="button" onClick={() => setRetry(value => value + 1)} disabled={busy}>重新检查连接</button>}
          </div>}
      </section>
      <footer className="footer"><span>Muskdeer</span><span>专注、可控、可追溯</span></footer>
    </main>
  </div>;
}

const root = document.getElementById('root');
if (!root) throw new Error('application root missing');
createRoot(root).render(<StrictMode><App /></StrictMode>);
