package adminhttp

import (
	"context"
	"net/http"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

func (r *Routes) requestEmergencyCancellation(ctx context.Context, request *app.RequestContext) {
	if !r.sameOrigin(request) {
		r.failure(request, domain.ErrForbidden)
		return
	}
	strategy, valid := strategyID(request)
	input, err := decodeEmergencyInput(request.Request.Body())
	keys := request.Request.Header.GetAll(IdempotencyHeader)
	if !valid || err != nil || len(keys) != 1 || !loginContentType(request) || len(request.URI().QueryString()) != 0 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	command, err := strategywire.EmergencyCommand(input, strategy, keys[0])
	if err != nil {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	csrf := request.Request.Header.GetAll(CSRFHeader)
	if len(csrf) != 1 || !cookieSecret(csrf[0]) {
		r.failure(request, domain.ErrForbidden)
		return
	}
	input.Reason = command.Reason
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.emergencyControls.RequestEmergencyCancellation(call, &strategyv1.RequestEmergencyCancellationRequest{
		Session:        session,
		CsrfToken:      csrf[0],
		IdempotencyKey: command.Key,
		StrategyId:     command.StrategyID,
		Input:          input,
	})
	if err == nil && !strategywire.ValidEmergencyResult(reply, command) {
		err = status.Error(codes.Unavailable, domain.MessageAdminControlUnavailable)
	}
	if err == nil {
		request.Response.Header.Set("Location", "/api/v1/strategies/"+strconv.FormatInt(strategy, 10)+"/emergency-cancellations/"+strconv.FormatInt(reply.Id, 10))
	}
	// An immutable acceptance replay does not sample the task's current state.
	r.strategyStatusReply(request, http.StatusAccepted, reply, err)
}

func (r *Routes) getEmergencyCancellation(ctx context.Context, request *app.RequestContext) {
	strategy, valid := strategyID(request)
	id, idValid := cancellationPathID(request.Param("cancellationID"))
	if !valid || !idValid || len(request.URI().QueryString()) != 0 || len(request.Request.Body()) != 0 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.emergencyQueries.GetEmergencyCancellation(call, &strategyv1.GetEmergencyCancellationRequest{
		Session:        session,
		StrategyId:     strategy,
		CancellationId: id,
	})
	if err == nil && !strategywire.ValidEmergencyObservation(reply, strategy, id) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func (r *Routes) listEmergencyCancellations(ctx context.Context, request *app.RequestContext) {
	strategy, valid := strategyID(request)
	page, pageValid := strategyPage(request)
	if !valid || !pageValid {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.emergencyQueries.ListEmergencyCancellations(call, &strategyv1.ListEmergencyCancellationsRequest{
		Session:    session,
		StrategyId: strategy,
		AfterId:    page.AfterID,
		Limit:      int32(page.Limit),
	})
	if err == nil && !strategywire.ValidEmergencyHistory(reply, strategy, page) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}
