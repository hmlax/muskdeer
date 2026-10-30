import type { CreationInput } from './creation-api';
import type { PublicationInput } from './publication-api';
import type { CancellationInput } from './cancellation-api';

export type Role = 'viewer' | 'operator' | 'admin';

export interface Session {
  user_id: string;
  username: string;
  role: Role;
  session_id: string;
  expires_at_ms: string;
  strategy_queries_enabled: boolean;
  strategy_controls_enabled: boolean;
}

export class ApiError extends Error {
  constructor(public readonly code: string) {
    super(code);
    this.name = 'ApiError';
  }
}

function sessionValue(value: unknown): Session {
  if (typeof value !== 'object' || value === null || !('session' in value)) throw new ApiError('INVALID_RESPONSE');
  const session = value.session;
  if (typeof session !== 'object' || session === null ||
      !('user_id' in session) || typeof session.user_id !== 'string' || !/^[1-9][0-9]*$/.test(session.user_id) ||
      !('username' in session) || typeof session.username !== 'string' ||
      !('role' in session) || typeof session.role !== 'string' || !['viewer', 'operator', 'admin'].includes(session.role) ||
      !('session_id' in session) || typeof session.session_id !== 'string' || !/^[1-9][0-9]*$/.test(session.session_id) ||
      !('expires_at_ms' in session) || typeof session.expires_at_ms !== 'string' || !/^[1-9][0-9]*$/.test(session.expires_at_ms)) {
    throw new ApiError('INVALID_RESPONSE');
  }
  return {
    user_id: session.user_id,
    username: session.username,
    role: session.role as Role,
    session_id: session.session_id,
    expires_at_ms: session.expires_at_ms,
    strategy_queries_enabled: 'strategy_queries_enabled' in session && session.strategy_queries_enabled === true,
    strategy_controls_enabled: 'strategy_controls_enabled' in session && session.strategy_controls_enabled === true,
  };
}

async function request(path: string, options: RequestInit = {}): Promise<Response> {
  const response = await fetch(`/api/v1/${path}`, {
    ...options,
    credentials: 'same-origin',
    cache: 'no-store',
  });
  if (!response.ok) {
    let code = 'UNAVAILABLE';
    try {
      const body: unknown = await response.json();
      if (typeof body === 'object' && body !== null && 'code' in body && typeof body.code === 'string') code = body.code;
    } catch { /* Protocol/transport errors have the same safe user-facing state. */ }
    throw new ApiError(code);
  }
  return response;
}

export async function currentSession(signal: AbortSignal): Promise<Session> {
  const response = await request('auth/session', { signal });
  const body: unknown = await response.json();
  return sessionValue(body);
}

export async function login(username: string, password: string): Promise<Session> {
  const response = await request('auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  const body: unknown = await response.json();
  return sessionValue(body);
}

function csrfProof(): string {
  const name = location.protocol === 'https:' ? '__Host-muskdeer_csrf' : 'muskdeer_csrf';
  const matching = document.cookie.split(';').map(cookie => cookie.trim()).filter(cookie => cookie.startsWith(`${name}=`));
  if (matching.length !== 1) throw new ApiError('FORBIDDEN');
  const proof = decodeURIComponent(matching[0].slice(name.length + 1));
  if (!/^[A-Za-z0-9_-]{43}$/.test(proof)) throw new ApiError('FORBIDDEN');
  return proof;
}

export async function logout(): Promise<void> {
  const proof = csrfProof();
  await request('auth/logout', { method: 'POST', headers: { 'X-CSRF-Token': proof } });
}

export async function managementWriteJSON(path: string, key: string, body: { action: string; expected_revision: string; reason: string } | CreationInput | PublicationInput | CancellationInput, signal: AbortSignal): Promise<unknown> {
  const response = await request(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfProof(), 'Idempotency-Key': key },
    body: JSON.stringify(body),
    signal,
  });
  return response.json();
}

export async function managementJSON(path: string, signal: AbortSignal): Promise<unknown> {
  const response = await request(path, { signal });
  return response.json();
}

export function errorText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.code) {
      case 'UNAUTHENTICATED': return '用户名或密码不正确，或账户暂时锁定。';
      case 'FORBIDDEN': return '当前请求未获授权，请刷新后重新登录。';
      case 'INVALID_REQUEST': return '请检查用户名和密码格式。';
    }
  }
  return '暂时无法连接管理服务，请稍后重试。';
}
