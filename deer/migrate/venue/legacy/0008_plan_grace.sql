-- Resting quotes survive this long past plan validity; 0 never clears them for
-- an expired plan. The default keeps the prior fixed 1.5s behaviour.
ALTER TABLE himalaya_budgets
  ADD COLUMN plan_grace_ms bigint NOT NULL DEFAULT 1500 CHECK (plan_grace_ms >= 0);
