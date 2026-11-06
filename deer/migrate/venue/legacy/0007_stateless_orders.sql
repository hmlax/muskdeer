-- Quote orders, cancellations and wash slots are no longer persisted; Himalaya
-- is the only record. Budget and profile policy rows are retained.
DROP TABLE himalaya_development_baselines;
DROP TABLE himalaya_cancellations;
DROP TABLE himalaya_intents;
DROP TABLE synthetic_tape_intents;
