package store

import (
	"database/sql"
	"time"
)

type DBClient struct {
	client *Client
	pool   *sql.DB
}

func NewDBClient(client *Client, pool *sql.DB) *DBClient {
	return &DBClient{
		client: client,
		pool:   pool,
	}
}
func (c *DBClient) Client() *Client { return c.client }
func (c *DBClient) DB() *sql.DB     { return c.pool }
func (c *DBClient) Close() error    { return c.pool.Close() }
func (c *DBClient) SetConnectionOption(idle, open int, lifetime time.Duration) {
	c.pool.SetMaxIdleConns(idle)
	c.pool.SetMaxOpenConns(open)
	c.pool.SetConnMaxLifetime(lifetime)
}
