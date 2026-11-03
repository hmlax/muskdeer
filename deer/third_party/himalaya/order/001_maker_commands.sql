-- Explicit, additive development migration. Never invoked by service startup.
CREATE TABLE IF NOT EXISTS himalaya_maker_schema (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    component VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    version INT UNSIGNED NOT NULL,
    createAt TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updateAt TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleteAt TIMESTAMP(6) NULL DEFAULT NULL,
    UNIQUE KEY maker_schema_component (component)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS himalaya_maker_command (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    fund_id BIGINT NOT NULL,
    symbol VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    client_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    order_id BIGINT UNSIGNED NOT NULL,
    business VARBINARY(4096) NOT NULL,
    authority VARBINARY(4096) NOT NULL,
    incarnation VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    response VARBINARY(4096) NULL,
    terminal BOOLEAN NOT NULL DEFAULT FALSE,
    createAt TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updateAt TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    deleteAt TIMESTAMP(6) NULL DEFAULT NULL,
    UNIQUE KEY maker_command_identity (user_id, fund_id, client_id),
    UNIQUE KEY maker_command_native_order (order_id),
    KEY maker_command_active (terminal, id)
) ENGINE=InnoDB;

INSERT INTO himalaya_maker_schema (component, version) VALUES ('maker_commands', 1)
ON DUPLICATE KEY UPDATE version = version;
