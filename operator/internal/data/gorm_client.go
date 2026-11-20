package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	// Register the configured SQLite driver.
	_ "modernc.org/sqlite"

	"database/sql"

	"github.com/XSAM/otelsql"
	// Register the configured MySQL driver.
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/lib/pq"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"github.com/hmlax/muskdeer/deer/management/runtimeconfig"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
)

// NewGormClient connects to the configured database without executing DDL.
// Persistence fields/tags and typed model operations are generated from proto.
func NewGormClient(ctx *bootstrap.Context) (*store.DBClient, func(), error) {
	cfg := ctx.GetConfig()
	if cfg == nil || cfg.Data == nil || cfg.Data.Database == nil {
		return nil, nil, errors.New("database configuration is required")
	}
	database := cfg.Data.Database
	driver := database.GetDriver()
	poolDriver := driver
	switch driver {
	case "postgresql", "pgx":
		poolDriver = "pgx"
	case "mariadb":
		poolDriver = "mysql"
	case "sqlite3":
		poolDriver = "sqlite"
	}
	var pool *sql.DB
	var err error
	if database.GetEnableTrace() {
		pool, err = otelsql.Open(poolDriver, database.GetSource(), otelsql.WithAttributes(attribute.String("db.system", driver)), otelsql.WithTracerProvider(runtimeconfig.TracingFromContext(ctx).Provider()), otelsql.WithSpanOptions(otelsql.SpanOptions{DisableQuery: true}))
	} else {
		pool, err = sql.Open(poolDriver, database.GetSource())
	}
	if err != nil {
		return nil, nil, fmt.Errorf("open GORM database pool: %w", err)
	}
	if database.GetEnableMetrics() {
		if _, err = otelsql.RegisterDBStatsMetrics(pool, otelsql.WithAttributes(attribute.String("db.system", driver))); err != nil {
			return nil, nil, errors.Join(fmt.Errorf("register database metrics: %w", err), pool.Close())
		}
	}
	var dialect gorm.Dialector
	switch driver {
	case "postgres", "postgresql", "pgx":
		dialect = postgres.New(postgres.Config{Conn: pool})
	case "mysql", "mariadb":
		dialect = mysql.New(mysql.Config{Conn: pool})
	case "sqlite", "sqlite3":
		dialect = sqlite.Dialector{
			DriverName: "sqlite",
			Conn:       pool,
		}
	default:
		return nil, nil, errors.Join(fmt.Errorf("unsupported database driver: %s", driver), pool.Close())
	}
	l := ctx.NewLoggerHelper("gorm/data/operator")
	db, err := gorm.Open(dialect, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		TranslateError: true,
		Logger: &auditGormLogger{
			log:     l,
			dialect: driver,
		},
	})
	if err != nil {
		if closeErr := pool.Close(); closeErr != nil {
			l.Errorf(ctx.Context(), "close failed database pool: %v", closeErr)
		}
		return nil, nil, fmt.Errorf("open GORM client: %w", err)
	}
	client := store.NewDBClient(store.NewClient(db), pool)
	if database.MaxIdleConnections != nil {
		client.DB().SetMaxIdleConns(int(database.GetMaxIdleConnections()))
	}
	if database.MaxOpenConnections != nil {
		client.DB().SetMaxOpenConns(int(database.GetMaxOpenConnections()))
	}
	if database.ConnectionMaxLifetime != nil {
		client.DB().SetConnMaxLifetime(database.GetConnectionMaxLifetime().AsDuration())
	}
	return client, func() {
		if closeErr := client.Close(); closeErr != nil {
			l.Errorf(ctx.Context(), "close GORM database: %v", closeErr)
		}
	}, nil
}

type auditGormLogger struct {
	log     *bLogger.Helper
	dialect string
}

func (l *auditGormLogger) LogMode(logger.LogLevel) logger.Interface { return l }
func (l *auditGormLogger) Info(ctx context.Context, message string, args ...any) {
	l.log.Infof(ctx, message, args...)
}
func (l *auditGormLogger) Warn(ctx context.Context, message string, args ...any) {
	l.log.Warnf(ctx, message, args...)
}
func (l *auditGormLogger) Error(ctx context.Context, message string, args ...any) {
	l.log.Errorf(ctx, message, args...)
}
func (l *auditGormLogger) Trace(ctx context.Context, begin time.Time, query func() (string, int64), err error) {
	statement, rows := query()
	write := isWriteStatement(statement)
	if !write {
		rows = -1
	}
	collectMasked(ctx, statement, time.Since(begin).Milliseconds(), l.dialect, write, rows)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		l.log.Errorf(ctx, "database operation failed: %v", err)
	}
}
