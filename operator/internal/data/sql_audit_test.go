// SQL auditing is collected by the GORM logger, including transactions and sink suppression.
package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/require"

	"github.com/hmlax/muskdeer/deer/management/audit"
	"github.com/hmlax/muskdeer/operator/internal/data/store"

	sqlutil "github.com/tx7do/go-utils/sqlutil"
)

// newAuditWrappedClient constructs a GORM client with the production audit logger.
// 与对应 accumulator 上下文（迁移用无 accumulator 的裸 ctx，避免 DDL 混入计量）。
func newAuditWrappedClient(t *testing.T) (*store.Client, context.Context, *[]audit.AuditEvent) {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:audit_%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", time.Now().UnixNano()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	database, err := gorm.Open(sqlite.Dialector{
		DriverName: "sqlite",
		Conn:       db,
	}, &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger: &auditGormLogger{
			log:     bLogger.NewHelper(bLogger.NopLogger()),
			dialect: "sqlite3",
		},
	})
	require.NoError(t, err)
	client := store.NewClient(database)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Schema.Create(context.Background()), "SQLite schema 迁移失败")

	events := make([]audit.AuditEvent, 0)
	accCtx := context.WithValue(context.Background(), audit.AccumulatorKey(), &events)
	return client, accCtx, &events
}

// sha256Hex 与 digest 实现一致性参照。
func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// TestAuditGorm_CollectsQueryEvent SELECT 查询经包装驱动必须产生
// 读型事件：方言 sqlite、IsWrite=false、AffectedRows=-1、文本为脱敏产物且摘要一致。
func TestAuditGorm_CollectsQueryEvent(t *testing.T) {
	client, accCtx, events := newAuditWrappedClient(t)

	_, err := client.Language.Query().All(accCtx)
	require.NoError(t, err, "空表查询返回空列表而非错误")
	// 查询已下发到驱动层，事件必须被采集
	require.NotEmpty(t, *events, "查询路径必须产生采集事件")
	for _, ev := range *events {
		require.Equal(t, "sqlite3", ev.Dialect)
		require.False(t, ev.IsWrite)
		require.Equal(t, int64(-1), ev.AffectedRows)
		require.True(t, ev.DataMasked)
		require.Equal(t, sqlutil.MaskingRules, ev.MaskingRules)
		require.Equal(t, sha256Hex(ev.SqlText), ev.SqlDigest, "摘要必须与脱敏文本一致")
	}
}

// TestAuditGorm_CollectsWriteEvent INSERT 经包装驱动必须产生
// 写型事件：方言 sqlite、IsWrite=true、文本脱敏且摘要一致。
func TestAuditGorm_CollectsWriteEvent(t *testing.T) {
	client, accCtx, events := newAuditWrappedClient(t)

	_ = client.Language.Create().
		SetLanguageCode("en-x-audit").
		SetLanguageName("English").
		SaveX(accCtx)

	require.NotEmpty(t, *events, "写入路径必须产生采集事件")
	writes := 0
	for _, ev := range *events {
		require.Equal(t, "sqlite3", ev.Dialect)
		require.True(t, ev.DataMasked)
		require.Equal(t, sqlutil.MaskingRules, ev.MaskingRules)
		require.Equal(t, sha256Hex(ev.SqlText), ev.SqlDigest)
		if ev.IsWrite {
			writes++
			require.Contains(t, []int64{-1, 1}, ev.AffectedRows, "写事件行数只允许 -1（取不到）或 1")
		}
	}
	require.NotZero(t, writes, "INSERT 必须产生至少一条写型事件（GORM 在 RETURNING 方言上经 Query 通道执行，isWriteStatement 负责纠偏标记）")
}

// TestAuditGorm_TxEvents 事务内语句必须产生实际数据库方言 的采集事件（读与写各一）。
func TestAuditGorm_TxEvents(t *testing.T) {
	client, accCtx, events := newAuditWrappedClient(t)

	tx, err := client.Tx(accCtx)
	require.NoError(t, err)
	_ = tx.Language.Create().
		SetLanguageCode("de-x-audit").
		SetLanguageName("Deutsch").
		SaveX(accCtx)
	_, _ = tx.Language.Query().All(accCtx)
	require.NoError(t, tx.Commit())

	txWrites, txReads := 0, 0
	for _, ev := range *events {
		if ev.Dialect != "sqlite3" {
			continue
		}
		require.True(t, ev.DataMasked)
		require.Equal(t, sqlutil.MaskingRules, ev.MaskingRules)
		require.Equal(t, sha256Hex(ev.SqlText), ev.SqlDigest)
		if ev.IsWrite {
			txWrites++
		} else {
			txReads++
			require.Equal(t, int64(-1), ev.AffectedRows)
		}
	}
	require.NotZero(t, txWrites, "事务内 INSERT 必须产生 事务写事件")
	require.NotZero(t, txReads, "事务内 SELECT 必须产生 事务读事件")
}

// TestAuditGorm_SinkAndNoAccumulator 防递归标记（IsSinking）与
// 无 accumulator 的上下文都必须跳过采集、不落事件、不 panic。
func TestAuditGorm_SinkAndNoAccumulator(t *testing.T) {
	client, _, events := newAuditWrappedClient(t)

	// 落库阶段标记 + accumulator：查询执行但不采集
	sinkEvents := make([]audit.AuditEvent, 0)
	sinkCtx := context.WithValue(
		context.WithValue(context.Background(), audit.AccumulatorKey(), &sinkEvents),
		audit.SinkKey(), true,
	)
	_, _ = client.Language.Query().All(sinkCtx)
	require.Empty(t, sinkEvents, "IsSinking 阶段不得采集")

	// 无 accumulator：执行不 panic、外部 events 不受影响
	_, _ = client.Language.Query().All(context.Background())
	require.Empty(t, *events, "无 accumulator 不得采集")
}

// TestDigest 摘要纯函数：与标准 SHA256 十六进制一致。
func TestDigest(t *testing.T) {
	require.Equal(t, sha256Hex("SELECT 1"), digest("SELECT 1"))
	require.Len(t, digest("x"), 64)
}

// TestIsWriteStatement 写型语句首关键字判定表：DML 关键字（含小写/前导空白/
// 前导括号变体）为写，其余（SELECT/DDL/空串/裸关键字名）为读。
func TestIsWriteStatement(t *testing.T) {
	writes := []string{
		"INSERT INTO t VALUES (1)",
		"insert into t values (1)",
		"  UPDATE t SET a=1",
		"\tDELETE FROM t",
		"(MERGE INTO t USING s ON 1=1)",
		"REPLACE INTO t VALUES (1)",
	}
	reads := []string{
		"SELECT * FROM t",
		"select 1",
		"CREATE TABLE t (a int)",
		"DROP TABLE t",
		"",
		"   ",
		"insertx into t",
	}
	for _, q := range writes {
		require.True(t, isWriteStatement(q), "%q 应判为写型", q)
	}
	for _, q := range reads {
		require.False(t, isWriteStatement(q), "%q 应判为读型", q)
	}
}

// rowsResultStub 实现 RowsAffected 接口的桩。
type rowsResultStub struct {
	n   int64
	err error
}

func (s rowsResultStub) RowsAffected() (int64, error) { return s.n, s.err }

// TestAffectedRows 行数提取的四个分支：正常值、错误、nil 载体、不实现接口的载体。
func TestAffectedRows(t *testing.T) {
	require.Equal(t, int64(5), affectedRows(rowsResultStub{n: 5}))
	require.Equal(t, int64(-1), affectedRows(rowsResultStub{err: context.Canceled}))
	require.Equal(t, int64(-1), affectedRows(nil))
	require.Equal(t, int64(-1), affectedRows(struct{}{}))
}
