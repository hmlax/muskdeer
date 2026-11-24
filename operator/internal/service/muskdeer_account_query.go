package service

import (
	"context"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"

	"github.com/hmlax/muskdeer/deer/management/muskdeer"
	pb "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	core "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	accountQueryInvalid    = "invalid account query"
	accountQueryIncomplete = "account list changed or is incomplete; refresh and retry"
	accountQueryPageSize   = 200
)

// ListAccounts applies account filters within the authenticated merchant scope.
// Console pagination is retained for ordinary lists. Filtered lists scan every
// scoped source page so their total and pagination describe all matching rows.
func (s *MuskdeerService) ListAccounts(ctx context.Context, in *pb.MuskdeerAccountListRequest) (*pb.MuskdeerAccountPage, error) {
	actor, err := s.actor(ctx, true)
	if err != nil {
		return nil, err
	}
	var userID int64
	if in.GetUserId() != "" {
		var valid bool
		userID, valid = muskdeer.TradingID(in.GetUserId())
		if !valid {
			return nil, s.failure(ctx, status.Error(codes.InvalidArgument, accountQueryInvalid))
		}
	}
	page, size := in.GetPage(), in.GetPageSize()
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = 20
	}
	if size > accountQueryPageSize || page > 10000000 {
		return nil, s.failure(ctx, status.Error(codes.InvalidArgument, accountQueryInvalid))
	}
	request := &core.ListRequest{
		Actor:     actor,
		Page:      page,
		PageSize:  size,
		Search:    in.GetSearch(),
		AccountId: in.GetAccountId(),
	}
	if userID == 0 && in.Enabled == nil {
		result, err := s.client.ListAccounts(ctx, request)
		if err != nil {
			return nil, s.failure(ctx, err)
		}
		return fromCoreAccountPage(result), nil
	}
	request.PageSize = accountQueryPageSize
	offset := uint64(page-1) * uint64(size)
	result := &pb.MuskdeerAccountPage{}
	var scanned, total uint64
	for request.Page = 1; ; request.Page++ {
		if err := ctx.Err(); err != nil {
			return nil, s.failure(ctx, status.FromContextError(err).Err())
		}
		source, err := s.client.ListAccounts(ctx, request)
		if err != nil {
			return nil, s.failure(ctx, err)
		}
		if source == nil || (request.Page > 1 && source.Total != total) {
			return nil, kratoserrors.Conflict(muskdeerErrorReason, accountQueryIncomplete)
		}
		total = source.Total
		scanned += uint64(len(source.Items))
		if scanned > total || (len(source.Items) == 0 && scanned < total) {
			return nil, kratoserrors.Conflict(muskdeerErrorReason, accountQueryIncomplete)
		}
		for _, account := range source.Items {
			if account == nil {
				return nil, kratoserrors.Conflict(muskdeerErrorReason, accountQueryIncomplete)
			}
			if (userID != 0 && account.UserId != userID) || (in.Enabled != nil && account.Enabled != *in.Enabled) {
				continue
			}
			if result.Total >= offset && uint32(len(result.Items)) < size {
				result.Items = append(result.Items, fromCoreAccount(account))
			}
			result.Total++
		}
		if scanned == total {
			return result, nil
		}
	}
}
