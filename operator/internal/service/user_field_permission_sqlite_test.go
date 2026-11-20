// user_field_permission.go 的集成测试（白盒，包内测试，无 DB 依赖）。
//
// 覆盖目标（FieldPermissionUserServiceServer 字段权限装饰器）：
//   - 读路径：List/Get 响应中命中隐藏字段集的字段被清除（protojson 不输出
//     未填充字段，清值即从 JSON 响应消失），未命中字段保持。
//   - 写路径：Create/Update 请求载荷中命中字段的值被剥离、field_mask 中
//     命中字段路径被同步剔除（"静默忽略"语义）。
//   - 透传路径：Delete/UserExists/EditUserPassword 不做裁剪。
//   - 无令牌上下文 / 无隐藏字段配置：完全不裁剪。
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-utils/trans"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

// innerUserServiceStub 是被装饰内层服务的本地桩：
// 嵌入 adminV1.UserServiceServer 接口获得默认方法集，只覆写装饰器会调用的
// 七个方法，并把收到的请求原样记录供断言。
type innerUserServiceStub struct {
	operatorV1.UserServiceServer

	gotListReq   *paginationV1.PagingRequest
	gotGetReq    *operatorV1.GetUserRequest
	gotCreateReq *operatorV1.CreateUserRequest
	gotUpdateReq *operatorV1.UpdateUserRequest
	gotDeleteReq *operatorV1.DeleteUserRequest
	gotExistsReq *operatorV1.UserExistsRequest
	gotPwdReq    *operatorV1.EditUserPasswordRequest
}

func (s *innerUserServiceStub) List(_ context.Context, in *paginationV1.PagingRequest) (*operatorV1.ListUserResponse, error) {
	s.gotListReq = in
	return &operatorV1.ListUserResponse{
		Items: []*operatorV1.User{
			{
				Nickname: trans.Ptr("桩用户甲"),
				Email:    trans.Ptr("stub-a@example.com"),
			},
			{
				Nickname: trans.Ptr("桩用户乙"),
				Email:    trans.Ptr("stub-b@example.com"),
			},
		},
		Total: 2,
	}, nil
}

func (s *innerUserServiceStub) Get(_ context.Context, in *operatorV1.GetUserRequest) (*operatorV1.User, error) {
	s.gotGetReq = in
	return &operatorV1.User{
		Nickname: trans.Ptr("桩用户甲"),
		Email:    trans.Ptr("stub-a@example.com"),
	}, nil
}

func (s *innerUserServiceStub) Create(_ context.Context, in *operatorV1.CreateUserRequest) (*emptypb.Empty, error) {
	s.gotCreateReq = in
	return &emptypb.Empty{}, nil
}

func (s *innerUserServiceStub) Update(_ context.Context, in *operatorV1.UpdateUserRequest) (*emptypb.Empty, error) {
	s.gotUpdateReq = in
	return &emptypb.Empty{}, nil
}

func (s *innerUserServiceStub) Delete(_ context.Context, in *operatorV1.DeleteUserRequest) (*emptypb.Empty, error) {
	s.gotDeleteReq = in
	return &emptypb.Empty{}, nil
}

func (s *innerUserServiceStub) UserExists(_ context.Context, in *operatorV1.UserExistsRequest) (*operatorV1.UserExistsResponse, error) {
	s.gotExistsReq = in
	return &operatorV1.UserExistsResponse{Exist: true}, nil
}

func (s *innerUserServiceStub) EditUserPassword(_ context.Context, in *operatorV1.EditUserPasswordRequest) (*emptypb.Empty, error) {
	s.gotPwdReq = in
	return &emptypb.Empty{}, nil
}

// hiddenCtx 构造带 User.email 隐藏字段声明的令牌上下文。
func hiddenCtx(ctx context.Context) context.Context {
	return auth.NewContext(ctx, &operatorV1.UserTokenPayload{
		UserId:       7,
		HiddenFields: []string{"User.email", "NotUser.email", "User."},
	})
}

// TestFieldPermissionUser_ListAndGetReadMask 读路径：命中字段清除、未命中保持。
func TestFieldPermissionUser_ListAndGetReadMask(t *testing.T) {
	inner := &innerUserServiceStub{}
	wrapped := NewFieldPermissionUserServiceServer(inner)
	ctx := hiddenCtx(context.Background())

	listResp, err := wrapped.List(ctx, &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Len(t, listResp.GetItems(), 2)
	for i, item := range listResp.GetItems() {
		require.Empty(t, item.GetEmail(), "第 %d 个用户的 email 应被隐藏字段集清除", i)
		require.NotEmpty(t, item.GetNickname(), "第 %d 个用户的未命中字段昵称应保持", i)
	}

	got, err := wrapped.Get(ctx, &operatorV1.GetUserRequest{})
	require.NoError(t, err)
	require.Empty(t, got.GetEmail(), "单条 Get 的 email 应被清除")
	require.NotEmpty(t, got.GetNickname(), "昵称应保持")
	require.NotNil(t, inner.gotGetReq, "内层应收到 Get 请求")
}

// TestFieldPermissionUser_ListWithoutTokenNoTrim 无令牌上下文：不裁剪。
func TestFieldPermissionUser_ListWithoutTokenNoTrim(t *testing.T) {
	inner := &innerUserServiceStub{}
	wrapped := NewFieldPermissionUserServiceServer(inner)

	listResp, err := wrapped.List(context.Background(), &paginationV1.PagingRequest{})
	require.NoError(t, err)
	require.Len(t, listResp.GetItems(), 2)
	require.Equal(t, "stub-a@example.com", listResp.GetItems()[0].GetEmail(),
		"无令牌上下文不裁剪（hiddenFields 为 nil）")
	require.Equal(t, "stub-b@example.com", listResp.GetItems()[1].GetEmail())

	got, err := wrapped.Get(context.Background(), &operatorV1.GetUserRequest{})
	require.NoError(t, err)
	require.Equal(t, "stub-a@example.com", got.GetEmail(), "无令牌上下文单条 Get 不裁剪")
}

// TestFieldPermissionUser_WriteStrip 写路径：Create 剥离命中字段值；
// Update 剥离值并剔除 field_mask 命中路径。
func TestFieldPermissionUser_WriteStrip(t *testing.T) {
	inner := &innerUserServiceStub{}
	wrapped := NewFieldPermissionUserServiceServer(inner)
	ctx := hiddenCtx(context.Background())

	_, err := wrapped.Create(ctx, &operatorV1.CreateUserRequest{
		Data: &operatorV1.User{
			Nickname: trans.Ptr("写路径昵称"),
			Email:    trans.Ptr("write-strip@example.com"),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, inner.gotCreateReq)
	require.Empty(t, inner.gotCreateReq.GetData().GetEmail(),
		"Create 载荷的 email 应被剥离后才到达内层")
	require.NotEmpty(t, inner.gotCreateReq.GetData().GetNickname(),
		"未命中字段 nickname 应保持")

	_, err = wrapped.Update(ctx, &operatorV1.UpdateUserRequest{
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"email", "nickname"}},
		Data: &operatorV1.User{
			Nickname: trans.Ptr("更新昵称"),
			Email:    trans.Ptr("update-strip@example.com"),
		},
	})
	require.NoError(t, err)
	require.NotNil(t, inner.gotUpdateReq)
	require.Empty(t, inner.gotUpdateReq.GetData().GetEmail(),
		"Update 载荷的 email 应被剥离")
	require.Equal(t, []string{"nickname"}, inner.gotUpdateReq.GetUpdateMask().GetPaths(),
		"field_mask 中命中的 email 路径应被剔除，nickname 保留")
}

// TestFieldPermissionUser_Passthrough 透传路径：Delete/UserExists/EditUserPassword
// 原样转交内层、无裁剪语义。
func TestFieldPermissionUser_Passthrough(t *testing.T) {
	inner := &innerUserServiceStub{}
	wrapped := NewFieldPermissionUserServiceServer(inner)
	ctx := hiddenCtx(context.Background())

	delReq := &operatorV1.DeleteUserRequest{}
	_, err := wrapped.Delete(ctx, delReq)
	require.NoError(t, err)
	require.Same(t, delReq, inner.gotDeleteReq, "Delete 应原样透传")

	existsReq := &operatorV1.UserExistsRequest{}
	existsResp, err := wrapped.UserExists(ctx, existsReq)
	require.NoError(t, err)
	require.Same(t, existsReq, inner.gotExistsReq, "UserExists 应原样透传")
	require.True(t, existsResp.GetExist(), "透传响应应保持内层返回值")

	pwdReq := &operatorV1.EditUserPasswordRequest{}
	_, err = wrapped.EditUserPassword(ctx, pwdReq)
	require.NoError(t, err)
	require.Same(t, pwdReq, inner.gotPwdReq, "EditUserPassword 应原样透传")
}
