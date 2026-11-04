ALTER TABLE himalaya_intents
  ADD COLUMN historical_approval_reference varchar(256) NOT NULL DEFAULT '',
  ADD COLUMN historical_snapshot jsonb NULL,
  ADD CONSTRAINT ck_himalaya_historical_approval
    CHECK ((historical_approval_reference = '' AND historical_snapshot IS NULL)
        OR (historical_approval_reference <> '' AND historical_snapshot IS NOT NULL));
