import { ApiError, managementWriteJSON } from './api';
import { integer, object, text } from './strategy-api';
import type { Source } from './strategy-api';
import type { ParameterInput } from './parameter-draft';

export interface PublicationInput { expected_revision: string; expected_config_version: string; market_data_source: Source; parameters: ParameterInput; reason: string }
export interface PublicationReceipt { id: string; strategy_id: string; previous_config_version: string; config_version: string; previous_revision: string; revision: string; market_data_source: Source; reason: string; created_at_ms: string }

export async function publishConfiguration(id: string, body: PublicationInput, key: string, signal: AbortSignal): Promise<PublicationReceipt> {
  integer(id); integer(body.expected_revision); integer(body.expected_config_version);
  const p = object(await managementWriteJSON(`strategies/${id}/configurations`, key, body, signal));
  integer(p.actor_user_id);
  const beforeRevision = integer(p.previous_revision); const revision = integer(p.revision); const beforeVersion = integer(p.previous_config_version); const version = integer(p.config_version);
  if (integer(p.strategy_id) !== id || beforeRevision !== body.expected_revision || BigInt(revision) !== BigInt(beforeRevision) + 1n || beforeVersion !== body.expected_config_version || BigInt(version) <= BigInt(beforeVersion) || p.market_data_source !== body.market_data_source || p.reason !== body.reason) throw new ApiError('INVALID_RESPONSE');
  return { id: integer(p.id), strategy_id: id, previous_config_version: beforeVersion, config_version: version, previous_revision: beforeRevision, revision, market_data_source: body.market_data_source, reason: text(p.reason), created_at_ms: integer(p.created_at_ms) };
}
