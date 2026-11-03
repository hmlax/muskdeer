//! Durable dispatch permission. An existing or uncertain claim is query-only.
use sqlx::{MySqlPool, Row, mysql::MySqlRow};

#[derive(Debug)]
pub enum Error {
    Database(sqlx::Error),
    Invalid,
    Conflict,
    MissingSchema,
    Corrupt,
}

impl From<sqlx::Error> for Error {
    fn from(value: sqlx::Error) -> Self {
        Self::Database(value)
    }
}

#[derive(Clone)]
pub struct Journal {
    pool: MySqlPool,
}

pub struct Command {
    pub user_id: i64,
    pub fund_id: i64,
    pub symbol: String,
    pub client_id: String,
    pub order_id: u64,
    /// Canonical typed protobuf business fields; proofs/trace/timeout excluded.
    pub business: Vec<u8>,
    /// Original typed authority envelope, retained for expiry cancellation.
    pub authority: Vec<u8>,
    pub incarnation: String,
}

#[derive(Debug)]
pub struct Record {
    pub id: u64,
    pub user_id: i64,
    pub fund_id: i64,
    pub symbol: String,
    pub client_id: String,
    pub order_id: u64,
    pub business: Vec<u8>,
    pub authority: Vec<u8>,
    pub incarnation: String,
    pub response: Option<Vec<u8>>,
    pub terminal: bool,
}

pub struct Reservation {
    /// True only after this invocation inserted and acknowledged the commit.
    pub fresh: bool,
    pub record: Record,
}

fn identity(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 128
        && value
            .bytes()
            .all(|c| c.is_ascii_alphanumeric() || b"._-:".contains(&c))
}

fn decode(row: MySqlRow) -> Result<Record, Error> {
    Ok(Record {
        id: row.try_get("id")?,
        user_id: row.try_get("user_id")?,
        fund_id: row.try_get("fund_id")?,
        symbol: row.try_get("symbol")?,
        client_id: row.try_get("client_id")?,
        order_id: row.try_get("order_id")?,
        business: row.try_get("business")?,
        authority: row.try_get("authority")?,
        incarnation: row.try_get("incarnation")?,
        response: row.try_get("response")?,
        terminal: row.try_get("terminal")?,
    })
}

impl Journal {
    /// Clone the existing OsPersist pool; this creates no additional pool.
    pub fn new(pool: MySqlPool) -> Self {
        Self { pool }
    }

    /// Startup checks only. The additive versioned SQL is applied explicitly.
    pub async fn verify_schema(&self) -> Result<(), Error> {
        let version: Option<u32> = sqlx::query_scalar("SELECT version FROM himalaya_maker_schema WHERE component='maker_commands' AND deleteAt IS NULL")
            .fetch_optional(&self.pool).await?;
        let unique: Option<String> = sqlx::query_scalar("SELECT GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='himalaya_maker_command' AND INDEX_NAME='maker_command_identity' AND NON_UNIQUE=0")
            .fetch_one(&self.pool).await?;
        if version != Some(1) || unique.as_deref() != Some("user_id,fund_id,client_id") {
            return Err(Error::MissingSchema);
        }
        sqlx::query("SELECT id,user_id,fund_id,symbol,client_id,order_id,business,authority,incarnation,response,terminal,createAt,updateAt,deleteAt FROM himalaya_maker_command LIMIT 0")
            .execute(&self.pool).await?;
        Ok(())
    }

    pub async fn reserve(&self, command: Command) -> Result<Reservation, Error> {
        if command.user_id <= 0
            || command.fund_id <= 0
            || command.order_id == 0
            || !identity(&command.client_id)
            || !identity(&command.incarnation)
            || command.symbol.is_empty()
            || command.symbol.len() > 32
            || !command
                .symbol
                .bytes()
                .all(|c| c.is_ascii_uppercase() || c.is_ascii_digit())
            || command.business.is_empty()
            || command.business.len() > 4096
            || command.authority.is_empty()
            || command.authority.len() > 4096
        {
            return Err(Error::Invalid);
        }
        let mut tx = self.pool.begin().await?;
        match sqlx::query("INSERT INTO himalaya_maker_command (user_id,fund_id,symbol,client_id,order_id,business,authority,incarnation) VALUES (?,?,?,?,?,?,?,?)")
            .bind(command.user_id).bind(command.fund_id).bind(&command.symbol).bind(&command.client_id).bind(command.order_id)
            .bind(&command.business).bind(&command.authority).bind(&command.incarnation).execute(&mut *tx).await {
                Ok(_) => {},
                Err(sqlx::Error::Database(ref error)) if error.is_unique_violation() => {
                    // A duplicate INSERT holds an InnoDB shared record lock.
                    // Release it before reading; concurrent duplicates must not
                    // all attempt to upgrade it with SELECT FOR UPDATE.
                    tx.rollback().await?;
                    let record = self.by_client(command.user_id, command.fund_id, &command.client_id)
                        .await?.ok_or(Error::Corrupt)?;
                    if record.symbol != command.symbol || record.business != command.business {
                        return Err(Error::Conflict);
                    }
                    return Ok(Reservation { fresh: false, record });
                },
                Err(error) => return Err(error.into()),
            };
        let row = sqlx::query("SELECT * FROM himalaya_maker_command WHERE user_id=? AND fund_id=? AND client_id=? AND deleteAt IS NULL FOR UPDATE")
            .bind(command.user_id).bind(command.fund_id).bind(&command.client_id).fetch_optional(&mut *tx).await?
            .ok_or(Error::Corrupt)?;
        let record = decode(row)?;
        if record.symbol != command.symbol || record.business != command.business {
            return Err(Error::Conflict);
        }
        // No dispatch permission escapes before durable commit acknowledgement.
        // An ambiguous commit error must return to the caller without dispatch.
        tx.commit().await?;
        Ok(Reservation {
            fresh: true,
            record,
        })
    }

    pub async fn by_client(
        &self,
        user: i64,
        fund: i64,
        client: &str,
    ) -> Result<Option<Record>, Error> {
        sqlx::query("SELECT * FROM himalaya_maker_command WHERE user_id=? AND fund_id=? AND client_id=? AND deleteAt IS NULL")
            .bind(user).bind(fund).bind(client).fetch_optional(&self.pool).await?.map(decode).transpose()
    }

    pub async fn by_order(
        &self,
        user: i64,
        fund: i64,
        order: u64,
    ) -> Result<Option<Record>, Error> {
        sqlx::query("SELECT * FROM himalaya_maker_command WHERE user_id=? AND fund_id=? AND order_id=? AND deleteAt IS NULL")
            .bind(user).bind(fund).bind(order).fetch_optional(&self.pool).await?.map(decode).transpose()
    }

    pub async fn observe(&self, order: u64, response: &[u8], terminal: bool) -> Result<(), Error> {
        if response.is_empty() || response.len() > 4096 {
            return Err(Error::Invalid);
        }
        let mut tx = self.pool.begin().await?;
        let row = sqlx::query("SELECT response,terminal FROM himalaya_maker_command WHERE order_id=? AND deleteAt IS NULL FOR UPDATE")
            .bind(order).fetch_optional(&mut *tx).await?.ok_or(Error::Corrupt)?;
        let prior_terminal: bool = row.try_get("terminal")?;
        let prior: Option<Vec<u8>> = row.try_get("response")?;
        if prior_terminal && (!terminal || prior.as_deref() != Some(response)) {
            return Err(Error::Conflict);
        }
        sqlx::query("UPDATE himalaya_maker_command SET response=?,terminal=? WHERE order_id=? AND deleteAt IS NULL")
            .bind(response).bind(terminal).bind(order).execute(&mut *tx).await?;
        tx.commit().await?;
        Ok(())
    }

    /// Cursor pagination bounds memory without silently discarding active keys.
    pub async fn active_page(&self, after: u64, limit: u32) -> Result<Vec<Record>, Error> {
        if limit == 0 || limit > 256 {
            return Err(Error::Invalid);
        }
        sqlx::query("SELECT * FROM himalaya_maker_command WHERE terminal=FALSE AND id>? AND deleteAt IS NULL ORDER BY id LIMIT ?")
            .bind(after).bind(limit).fetch_all(&self.pool).await?.into_iter().map(decode).collect()
    }
}
