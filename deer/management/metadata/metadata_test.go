package metadata

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/go-kratos/kratos/v2/metadata"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/stretchr/testify/assert"
)

func TestNewContext_FromContext_RoundTrip(t *testing.T) {
	// 使用空的 OperatorMetadata 做回环测试
	info := &operatorV1.OperatorMetadata{}
	b, _ := encodeOperatorMetadata(info)
	ctx2 := metadata.NewServerContext(context.Background(), metadata.Metadata{
		mdOperatorKey: []string{b},
	})

	_, err := FromContext(ctx2)
	assert.Equal(t, err, ErrNoOperatorHeader)
}

func TestFromContext_MissingOrInvalid(t *testing.T) {
	// 丢失 header
	got, err := FromContext(context.Background())
	assert.Error(t, err)
	assert.Nil(t, got)

	// 无效 base64
	ctx := metadata.AppendToClientContext(context.Background(), mdOperatorKey, "not-base64")
	got, err = FromContext(ctx)
	assert.Error(t, err)
	assert.Nil(t, got)

	// 有效 base64 但不是有效的 proto bytes
	bad := base64.RawStdEncoding.EncodeToString([]byte("not-proto-bytes"))
	ctx = metadata.AppendToClientContext(context.Background(), mdOperatorKey, bad)
	got, err = FromContext(ctx)
	assert.Error(t, err)
	assert.Nil(t, got)
}

func TestNewContext_FromContext_InvalidData(t *testing.T) {
	// 使用空的 OperatorMetadata 做回环测试
	info := &operatorV1.OperatorMetadata{
		UserId:    uint64(123),
		TenantId:  uint64(456),
		OrgUnitId: uint64(789),
		DataScope: operatorV1.DataScope_ALL,
		RoleIds:   []uint64{1},
	}
	b, _ := encodeOperatorMetadata(info)
	ctx2 := metadata.NewServerContext(context.Background(), metadata.Metadata{
		mdOperatorKey: []string{b},
	})

	got, err := FromContext(ctx2)
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, info.UserId, got.UserId)
	assert.Equal(t, info.TenantId, got.TenantId)
	assert.Equal(t, info.OrgUnitId, got.OrgUnitId)
	assert.Equal(t, info.DataScope, got.DataScope)
	assert.Equal(t, info.RoleIds, got.RoleIds)
}

func TestNewOperatorMetadataContext_WriteAndRead(t *testing.T) {
	info := &operatorV1.OperatorMetadata{
		UserId:    uint64(123),
		TenantId:  uint64(456),
		OrgUnitId: uint64(789),
		DataScope: operatorV1.DataScope_ALL,
		RoleIds:   []uint64{1},
	}
	ctx, err := NewContext(context.Background(), info)
	assert.NoError(t, err)

	md, ok := metadata.FromClientContext(ctx)
	assert.True(t, ok)

	op := md.Get(mdOperatorKey)
	assert.NotEmpty(t, op)

	got, err := decodeOperatorMetadata(op)
	assert.NoError(t, err)

	if assert.NotNil(t, got.UserId) {
		assert.Equal(t, info.UserId, got.UserId)
	}
	if assert.NotNil(t, got.TenantId) {
		assert.Equal(t, info.TenantId, got.TenantId)
	}
	if assert.NotNil(t, got.OrgUnitId) {
		assert.Equal(t, info.OrgUnitId, got.OrgUnitId)
	}
	if assert.NotNil(t, got.DataScope) {
		assert.Equal(t, info.DataScope, got.DataScope)
	}
	if assert.NotNil(t, got.RoleIds) {
		assert.Equal(t, info.RoleIds, got.RoleIds)
	}
}
