-- Preserve all order identities and reservations. Support both uncertain sends
-- and ACKs newer than a venue observation without reading old ACK heap pages.
CREATE INDEX idx_venue_intent_current_reservation
ON venue_order_intents (account_id, state, update_at)
INCLUDE (reserved_notional)
WHERE delete_at IS NULL;
