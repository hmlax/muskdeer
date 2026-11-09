-- Current instance execution uses matching funds/orders and no process leases.
-- Retain every historical row and value. No reservation/owner data is reset.
ALTER TABLE venue_order_intents DROP CONSTRAINT venue_order_intents_owner_epoch_check;
ALTER TABLE venue_order_intents DROP CONSTRAINT venue_order_intents_reserved_notional_check;
ALTER TABLE venue_order_intents ADD CONSTRAINT venue_order_intents_owner_epoch_check CHECK (
    (owner_epoch > 0 AND reserved_notional > 0)
    OR (owner_epoch = 0 AND owner_id = '' AND strategy_instance_id > 0 AND strategy_revision > 0 AND reserved_notional = 0)
);
ALTER TABLE venue_cancellations DROP CONSTRAINT venue_cancellations_owner_epoch_check;
ALTER TABLE venue_cancellations ADD CONSTRAINT venue_cancellations_owner_epoch_check CHECK (
    owner_epoch > 0 OR (owner_epoch = 0 AND owner_id = '')
);
ALTER TABLE venue_account_snapshots DROP CONSTRAINT himalaya_projections_publisher_epoch_check;
ALTER TABLE venue_account_snapshots ADD CONSTRAINT himalaya_projections_publisher_epoch_check CHECK (
    publisher_epoch > 0 OR (publisher_epoch = 0 AND publisher_owner_id = '')
);
ALTER TABLE trading_account_policies DROP CONSTRAINT himalaya_budgets_account_id_fkey;
ALTER TABLE venue_cancellations DROP CONSTRAINT venue_cancellations_account_id_fkey;
