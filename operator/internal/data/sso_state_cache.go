package data

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
)

const (
	// SsoStateTTL state 的有效期：用户在 IdP 登录页停留的时间窗。
	SsoStateTTL = 10 * time.Minute

	// ssoStateKeyFmt state 键前缀：sso:state:<state>
)

// ErrSsoStateNotFound state 不存在/过期/已被使用（防 CSRF 与回调重放）。
var ErrSsoStateNotFound = errors.New("sso state not found or expired")

// SsoStateCache OIDC 登录 state 缓存。
// 写入于 GetSsoLoginUrl（生成跳转 URL 时），取删于 SsoLogin（回调换令牌时）——
// verify-and-delete 单次有效，与 mfaChallengeCache 同模式。
// 走 Redis 而非进程内存：授权跳转与回调可能落在多实例部署的不同节点上。
type SsoStateCache struct {
	log *bLogger.Helper
	rdb *redis.Client
}

func NewSsoStateCache(ctx *bootstrap.Context, rdb *redis.Client) *SsoStateCache {
	return &SsoStateCache{
		rdb: rdb,
		log: ctx.NewLoggerHelper("sso-state/cache"),
	}
}

// Put 记录一个 state，TTL 见 SsoStateTTL。
func (c *SsoStateCache) Put(ctx context.Context, state string) error {
	key := "sso:state:" + state
	if err := c.rdb.Set(ctx, key, 1, SsoStateTTL).Err(); err != nil {
		c.log.Errorf(ctx, "set sso state failed: %s", err.Error())
		return err
	}
	return nil
}

// Take 验证并取删 state：存在返回 true；不存在/过期/已用返回 ErrSsoStateNotFound。
func (c *SsoStateCache) Take(ctx context.Context, state string) (bool, error) {
	key := "sso:state:" + state
	deleted, err := c.rdb.Del(ctx, key).Result()
	if err != nil {
		c.log.Errorf(ctx, "take sso state failed: %s", err.Error())
		return false, err
	}
	if deleted == 0 {
		return false, ErrSsoStateNotFound
	}
	return true, nil
}
