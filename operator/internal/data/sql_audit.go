package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/hmlax/muskdeer/deer/management/audit"

	sqlutil "github.com/tx7do/go-utils/sqlutil"
)

// isWriteStatement recognizes DML even when a driver uses RETURNING through its query path.
func isWriteStatement(query string) bool {
	q := strings.TrimLeft(query, " \t\r\n(")
	w := q
	if i := strings.IndexAny(q, " \t\r\n("); i >= 0 {
		w = q[:i]
	}
	switch strings.ToUpper(w) {
	case "INSERT", "UPDATE", "DELETE", "REPLACE", "MERGE":
		return true
	}
	return false
}

// collect 从 ctx 取 accumulator，append 一条事件。无 accumulator 或落库中则跳过。
func collect(ctx context.Context, ev audit.AuditEvent) {
	if audit.IsSinking(ctx) {
		return
	}
	acc, ok := audit.FromContext(ctx)
	if !ok || acc == nil {
		return
	}
	*acc = append(*acc, ev)
}

// collectMasked 对 SQL 做字面量脱敏后采集事件。sql_text 存脱敏文本；
// sql_digest 基于脱敏文本计算——同构不同参的 SQL 指纹一致，便于按语句分组。
func collectMasked(ctx context.Context, query string, latency int64, dialectName string, isWrite bool, rows int64) {
	masked := sqlutil.MaskSQL(query)
	collect(ctx, audit.AuditEvent{
		SqlText:      masked,
		SqlDigest:    digest(masked),
		Latency:      latency,
		Dialect:      dialectName,
		IsWrite:      isWrite,
		AffectedRows: rows,
		DataMasked:   true,
		MaskingRules: sqlutil.MaskingRules,
	})
}

// digest 返回 SQL 文本的 SHA256 摘要（十六进制），用于 sql_digest 字段。
func digest(query string) string {
	h := sha256.Sum256([]byte(query))
	return hex.EncodeToString(h[:])
}

// affectedRows returns database/sql affected rows when available.
func affectedRows(v any) int64 {
	if v == nil {
		return -1
	}
	type rowsAffected interface {
		RowsAffected() (int64, error)
	}
	if r, ok := v.(rowsAffected); ok {
		n, err := r.RowsAffected()
		if err != nil {
			return -1
		}
		return n
	}
	return -1
}
