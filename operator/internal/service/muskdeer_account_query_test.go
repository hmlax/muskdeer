package service

import (
	"context"
	"errors"
	"testing"
	"time"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/stretchr/testify/require"
	"github.com/tx7do/go-crud/viewer"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	"github.com/hmlax/muskdeer/deer/management/muskdeer"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	core "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/operator/internal/data"
	"github.com/hmlax/muskdeer/operator/internal/data/storetest"
)

type accountQueryCore struct {
	core.ConsoleClient
	rows  []*core.Account
	calls []*core.ListRequest
	reply func(context.Context, *core.ListRequest) (*core.AccountPage, error)
}

func (c *accountQueryCore) ListAccounts(ctx context.Context, in *core.ListRequest, _ ...grpc.CallOption) (*core.AccountPage, error) {
	c.calls = append(c.calls, proto.CloneOf(in))
	if c.reply != nil {
		return c.reply(ctx, in)
	}
	start := int(in.GetPage()-1) * 2
	end := min(start+2, len(c.rows))
	if start >= len(c.rows) {
		return &core.AccountPage{Total: uint64(len(c.rows))}, nil
	}
	return &core.AccountPage{
		Items: c.rows[start:end],
		Total: uint64(len(c.rows)),
	}, nil
}
func TestMuskdeerAccountQueryFiltersBeforePaginationAndKeepsScope(t *testing.T) {
	db := storetest.NewClientForTest(t)
	system := storetest.NewSystemContext(t.Context())
	client := &accountQueryCore{rows: []*core.Account{
		{
			Id:              1,
			AccountId:       muskdeer.AccountScope(9007199254740993, 1),
			UserId:          9007199254740993,
			Enabled:         true,
			MaximumNotional: "9007199254740993.123456789012345678",
		},
		{
			Id:        2,
			AccountId: muskdeer.AccountScope(7, 2),
			UserId:    7,
			Enabled:   false,
		},
		{
			Id:        3,
			AccountId: muskdeer.AccountScope(9007199254740993, 3),
			UserId:    9007199254740993,
			Enabled:   false,
		},
		{
			Id:        4,
			AccountId: muskdeer.AccountScope(9007199254740993, 4),
			UserId:    9007199254740993,
			Enabled:   true,
		},
	}}
	for _, row := range client.rows {
		_, err := db.Client().MuskdeerAccountBinding.Create().SetTenantID(7).SetAccountID(row.AccountId).SetIsEnabled(true).SetVersion(1).SetCreatedAt(time.Now().UTC()).SetUpdatedAt(time.Now().UTC()).Save(system)
		require.NoError(t, err)
	}
	service := &MuskdeerService{
		client:   client,
		bindings: data.NewMuskdeerBindingRepo(db),
		log:      bLogger.NewHelper(bLogger.NopLogger()),
	}
	tenant := uint32(7)
	ctx := auth.NewContext(viewer.WithContext(t.Context(), viewer.NewUserContext(42, 7, 0, "", nil)), &pb.UserTokenPayload{
		UserId:   42,
		TenantId: &tenant,
	})
	for _, tc := range []struct {
		name    string
		enabled *bool
		user    string
		page    uint32
		total   uint64
		want    int64
	}{
		{
			name:  "all user accounts",
			user:  "9007199254740993",
			page:  1,
			total: 3,
			want:  1,
		},
		{
			name:    "disabled user account",
			user:    "9007199254740993",
			enabled: proto.Bool(false),
			page:    1,
			total:   1,
			want:    3,
		},
		{
			name:    "enabled user page two",
			user:    "9007199254740993",
			enabled: proto.Bool(true),
			page:    2,
			total:   2,
			want:    4,
		},
		{
			name:    "disabled across all users",
			enabled: proto.Bool(false),
			page:    1,
			total:   2,
			want:    2,
		},
		{
			name: "no matching user",
			user: "8",
			page: 1,
		},
		{
			name:  "beyond filtered last page",
			user:  "9007199254740993",
			page:  4,
			total: 3,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client.calls = nil
			result, err := service.ListAccounts(ctx, &pb.MuskdeerAccountListRequest{
				UserId:    tc.user,
				Enabled:   tc.enabled,
				Page:      tc.page,
				PageSize:  1,
				Search:    "USDT",
				AccountId: "scoped/filter",
			})
			require.NoError(t, err)
			require.Equal(t, tc.total, result.Total)
			if tc.want == 0 {
				require.Empty(t, result.Items)
			} else {
				require.Len(t, result.Items, 1)
				require.Equal(t, tc.want, result.Items[0].Id)
				if tc.want == 1 {
					require.Equal(t, client.rows[0].MaximumNotional, result.Items[0].MaximumNotional)
				}
			}
			require.Len(t, client.calls, 2)
			for _, call := range client.calls {
				require.Equal(t, uint64(7), call.Actor.TenantId)
				require.Len(t, call.Actor.AllowedAccountIds, 4)
				require.Equal(t, "USDT", call.Search)
				require.Equal(t, "scoped/filter", call.AccountId)
				require.Equal(t, uint32(200), call.PageSize)
			}
		})
	}
	for _, invalid := range []string{"-1", "0", "1.2", "01", "9223372036854775808"} {
		before := len(client.calls)
		_, err := service.ListAccounts(ctx, &pb.MuskdeerAccountListRequest{UserId: invalid})
		require.Equal(t, 400, int(kratoserrors.Code(err)))
		require.Len(t, client.calls, before)
	}
	for _, invalid := range []*pb.MuskdeerAccountListRequest{
		{PageSize: 201},
		{Page: 10000001},
	} {
		before := len(client.calls)
		_, err := service.ListAccounts(ctx, invalid)
		require.Equal(t, 400, int(kratoserrors.Code(err)))
		require.Len(t, client.calls, before)
	}
	t.Run("unfiltered keeps core pagination", func(t *testing.T) {
		client.calls = nil
		_, err := service.ListAccounts(ctx, &pb.MuskdeerAccountListRequest{
			Page:      2,
			PageSize:  1,
			Search:    "ETH",
			AccountId: "scoped/filter",
		})
		require.NoError(t, err)
		require.Len(t, client.calls, 1)
		require.Equal(t, uint32(2), client.calls[0].Page)
		require.Equal(t, uint32(1), client.calls[0].PageSize)
		require.Equal(t, "ETH", client.calls[0].Search)
		require.Equal(t, "scoped/filter", client.calls[0].AccountId)
	})
	for _, tc := range []struct {
		name  string
		reply func(context.Context, *core.ListRequest) (*core.AccountPage, error)
	}{
		{
			name: "empty incomplete page",
			reply: func(context.Context, *core.ListRequest) (*core.AccountPage, error) {
				return &core.AccountPage{Total: 1}, nil
			},
		},
		{
			name:  "nil source",
			reply: func(context.Context, *core.ListRequest) (*core.AccountPage, error) { return nil, nil },
		},
		{
			name: "core failure",
			reply: func(context.Context, *core.ListRequest) (*core.AccountPage, error) {
				return nil, errors.New("unavailable")
			},
		},
		{
			name: "changed total on second page",
			reply: func(_ context.Context, r *core.ListRequest) (*core.AccountPage, error) {
				total := uint64(2)
				if r.Page > 1 {
					total = 3
				}
				return &core.AccountPage{
					Items: client.rows[:1],
					Total: total,
				}, nil
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client.reply = tc.reply
			result, err := service.ListAccounts(ctx, &pb.MuskdeerAccountListRequest{Enabled: proto.Bool(false)})
			require.Error(t, err)
			require.Nil(t, result, "never return partial matches after a source failure")
		})
	}
	t.Run("cancellation between pages", func(t *testing.T) {
		requestCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		client.calls = nil
		client.reply = func(context.Context, *core.ListRequest) (*core.AccountPage, error) {
			cancel()
			return &core.AccountPage{
				Items: client.rows[:1],
				Total: 2,
			}, nil
		}
		result, err := service.ListAccounts(requestCtx, &pb.MuskdeerAccountListRequest{Enabled: proto.Bool(true)})
		require.Error(t, err)
		require.Nil(t, result)
		require.Len(t, client.calls, 1)
	})
}
