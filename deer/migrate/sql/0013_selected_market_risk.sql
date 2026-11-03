-- Existing grants have no market provenance. Upgrade closes them rather than
-- fabricating a source, publisher epoch or price from legacy risk metadata.
ALTER TABLE strategy_instances ADD COLUMN market_mode varchar(16) NOT NULL DEFAULT 'REPLAY'
    CHECK (market_mode IN ('LIVE', 'REPLAY'));
CREATE FUNCTION keep_strategy_market_mode() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.market_mode <> OLD.market_mode THEN
        RAISE EXCEPTION USING ERRCODE = '23514', MESSAGE = 'strategy market mode is immutable';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER strategy_market_mode BEFORE UPDATE ON strategy_instances
    FOR EACH ROW EXECUTE FUNCTION keep_strategy_market_mode();

ALTER TABLE risk_snapshots
    ADD COLUMN market_feed_id bigint NULL REFERENCES market_feeds(id),
    ADD COLUMN market_revision bigint NOT NULL DEFAULT 0 CHECK (market_revision >= 0),
    ADD COLUMN market_source_version bigint NOT NULL DEFAULT 0 CHECK (market_source_version >= 0),
    ADD COLUMN market_maximum_age_ms bigint NOT NULL DEFAULT 5000 CHECK (market_maximum_age_ms BETWEEN 1 AND 5000);
UPDATE risk_snapshots SET allow_new_risk = false, revision = revision + 1,
    update_at = floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint;
ALTER TABLE risk_snapshots ADD CONSTRAINT ck_risk_market_provenance CHECK (
    NOT allow_new_risk OR (market_feed_id IS NOT NULL AND market_revision > 0 AND market_source_version > 0)
);
ALTER TABLE risk_snapshots DROP CONSTRAINT risk_snapshots_market_sequence_check;
ALTER TABLE risk_snapshots ADD CONSTRAINT risk_snapshots_market_sequence_check CHECK (market_sequence >= 0);

-- Separate deferred gate keeps the earlier funds/intent fence intact. CANCEL
-- remains available even after selected market loss or risk grant closure.
CREATE FUNCTION validate_simulator_selected_market() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    risk risk_snapshots%ROWTYPE;
    strategy strategy_instances%ROWTYPE;
    feed market_feeds%ROWTYPE;
    publisher market_publisher_leases%ROWTYPE;
    book market_snapshots%ROWTYPE;
    now_ms bigint;
    age_ms bigint;
    skew_ms bigint;
BEGIN
    IF NEW.intent_id IS NULL OR NEW.action = 'CANCEL' THEN
        RETURN NEW;
    END IF;
    SELECT * INTO risk FROM risk_snapshots WHERE strategy_instance_id = NEW.strategy_instance_id FOR SHARE;
    SELECT * INTO strategy FROM strategy_instances WHERE id = NEW.strategy_instance_id FOR SHARE;
    SELECT * INTO feed FROM market_feeds WHERE id = risk.market_feed_id FOR SHARE;
    -- Same lock order as the publisher: lease before current snapshot.
    SELECT * INTO publisher FROM market_publisher_leases WHERE market_feed_id = feed.id FOR SHARE;
    SELECT * INTO book FROM market_snapshots WHERE market_feed_id = feed.id FOR SHARE;
    now_ms := floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint;
    age_ms := LEAST(risk.market_maximum_age_ms, (feed.policy->>'maximum_age_ms')::bigint);
    skew_ms := COALESCE((feed.policy->>'maximum_skew_ms')::bigint, 0);
    IF (risk.id IS NOT NULL AND risk.delete_at IS NULL AND risk.allow_new_risk AND risk.reconciled
        AND risk.observed_at <= now_ms AND risk.expires_at > now_ms AND now_ms - risk.observed_at <= 5000
        AND strategy.id IS NOT NULL AND strategy.delete_at IS NULL
        AND feed.id IS NOT NULL AND feed.delete_at IS NULL AND feed.source = risk.market_data_source
        AND feed.source IS NOT DISTINCT FROM strategy.active_market_data_source
        AND feed.instrument_id = NEW.instrument_id AND feed.mode = strategy.market_mode
        AND feed.source_version = risk.market_source_version
        AND publisher.id IS NOT NULL AND publisher.delete_at IS NULL AND publisher.expires_at > now_ms
        AND book.id IS NOT NULL AND book.delete_at IS NULL AND book.quality = 'HEALTHY' AND book.reason = ''
        AND book.publisher_owner_id = publisher.owner_id AND book.publisher_epoch = publisher.epoch
        AND book.publisher_epoch = risk.market_epoch AND book.revision >= risk.market_revision
        AND book.valid_until > now_ms AND book.event_at > 0 AND book.received_at > 0
        AND now_ms - book.event_at < age_ms AND now_ms - book.received_at < age_ms
        AND book.event_at <= now_ms + skew_ms AND book.received_at <= now_ms + skew_ms AND book.evaluated_at <= now_ms + skew_ms
        AND book.bid_price > 0 AND book.ask_price > book.bid_price AND book.bid_quantity > 0 AND book.ask_quantity > 0
        AND book.mid_price * 2 = book.bid_price + book.ask_price
        AND (book.ask_price - book.bid_price) * 10000 <= book.mid_price * (feed.policy->>'maximum_spread_bps')::numeric
    ) IS NOT TRUE THEN
        RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'selected market risk rejected', CONSTRAINT = 'simulator_market_risk';
    END IF;
    RETURN NEW;
END;
$$;
CREATE CONSTRAINT TRIGGER simulator_market_risk AFTER INSERT ON simulator_commands
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_simulator_selected_market();
