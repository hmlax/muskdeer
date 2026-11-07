ALTER TABLE synthetic_tape_intents
  DROP CONSTRAINT synthetic_tape_intents_sequence_check;
ALTER TABLE synthetic_tape_intents
  ADD CONSTRAINT synthetic_tape_intents_sequence_check CHECK (sequence >= 0);
