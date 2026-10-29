import { ApiError, managementJSON } from './api';
import type { Role } from './api';
import { integer, object, text } from './strategy-api';

export interface AuditFilters { actor: string; action: string; resource: string }
export interface AuditEntry {
  id: string; actor: string; actor_role: Role; action: string; resource: string; reason: string;
  before_version: string | null; after_version: string | null; created_at_ms: string;
}
export interface AuditPage { audits: AuditEntry[]; next_after_id: string }
export const emptyAuditFilters: AuditFilters = { actor: '', action: '', resource: '' };

export function validAuditFilters(filters: AuditFilters): boolean {
  return (Object.entries(filters) as [keyof AuditFilters, string][]).every(([name, value]) => value.trim() === value && !/[\u0000-\u001f\u007f-\u009f]/.test(value) && new TextEncoder().encode(value).length <= (name === 'resource' ? 128 : 64));
}
export function auditFilterURL(filters: AuditFilters): string {
  const query = new URLSearchParams();
  if (filters.actor) query.set('actor', filters.actor);
  if (filters.action) query.set('action', filters.action);
  if (filters.resource) query.set('resource', filters.resource);
  return query.toString();
}
export function initialAuditFilters(): AuditFilters | null {
  if (location.search.length > 1025) return null;
  const query = new URLSearchParams(location.search);
  if ([...query.keys()].some(key => !['actor', 'action', 'resource'].includes(key) || query.getAll(key).length !== 1)) return null;
  const filters = { actor: query.get('actor') ?? '', action: query.get('action') ?? '', resource: query.get('resource') ?? '' };
  return validAuditFilters(filters) ? filters : null;
}
function auditText(value: unknown, maximum: number): string {
  const result = text(value);
  if (!result || result.includes('\0') || new TextEncoder().encode(result).length > maximum) throw new ApiError('INVALID_RESPONSE');
  return result;
}
function audit(value: unknown): AuditEntry {
  const p = object(value);
  if (p.actor_role !== 'viewer' && p.actor_role !== 'operator' && p.actor_role !== 'admin') throw new ApiError('INVALID_RESPONSE');
  return {
    id: integer(p.id), actor: auditText(p.actor, 64), actor_role: p.actor_role, action: auditText(p.action, 64), resource: auditText(p.resource, 128), reason: auditText(p.reason, 256),
    before_version: p.before_version == null ? null : integer(p.before_version), after_version: p.after_version == null ? null : integer(p.after_version), created_at_ms: integer(p.created_at_ms),
  };
}
export async function listAudits(filters: AuditFilters, after: string, signal: AbortSignal): Promise<AuditPage> {
  integer(after, true);
  if (!validAuditFilters(filters)) throw new ApiError('INVALID_REQUEST');
  const p = object(await managementJSON(`audits?limit=20&after_id=${after}&${auditFilterURL(filters)}`, signal));
  if (!Array.isArray(p.audits) || p.audits.length > 20) throw new ApiError('INVALID_RESPONSE');
  const audits = p.audits.map(audit); const next = integer(p.next_after_id, true);
  let previous = BigInt(after);
  for (const entry of audits) {
    if (BigInt(entry.id) <= previous || filters.actor && filters.actor !== entry.actor || filters.action && filters.action !== entry.action || filters.resource && filters.resource !== entry.resource) throw new ApiError('INVALID_RESPONSE');
    previous = BigInt(entry.id);
  }
  if (next !== '0' && (audits.length !== 20 || next !== audits[audits.length - 1].id)) throw new ApiError('INVALID_RESPONSE');
  return { audits, next_after_id: next };
}
export async function getAudit(id: string, signal: AbortSignal): Promise<AuditEntry> {
  integer(id);
  const p = object(await managementJSON(`audits/${id}`, signal)); const entry = audit(p.audit);
  if (entry.id !== id) throw new ApiError('INVALID_RESPONSE');
  return entry;
}
