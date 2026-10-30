package adminhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/cloudwego/hertz/pkg/app"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

func (r *Routes) requestOrderCancellation(ctx context.Context, request *app.RequestContext) {
	if !r.sameOrigin(request) {
		r.failure(request, domain.ErrForbidden)
		return
	}
	strategy, valid := strategyID(request)
	order, orderValid := cancellationPathID(request.Param("orderID"))
	input, err := decodeCancellationInput(request.Request.Body())
	keys := request.Request.Header.GetAll(IdempotencyHeader)
	if !valid || !orderValid || err != nil || len(keys) != 1 || !loginContentType(request) || len(request.URI().QueryString()) != 0 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	command, err := strategywire.CancellationCommand(input, strategy, order, keys[0])
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
	reply, err := r.cancellationControls.RequestOrderCancellation(call, &strategyv1.RequestOrderCancellationRequest{
		Session:        session,
		CsrfToken:      csrf[0],
		IdempotencyKey: command.Key,
		StrategyId:     command.StrategyID,
		OrderId:        command.OrderID,
		Input:          input,
	})
	if err == nil && !strategywire.ValidCancellationResult(reply, command) {
		err = status.Error(codes.Unavailable, domain.MessageAdminControlUnavailable)
	}
	responseStatus := http.StatusOK
	if err == nil {
		request.Response.Header.Set("Location", "/api/v1/strategies/"+strconv.FormatInt(strategy, 10)+"/order-cancellations/"+strconv.FormatInt(reply.Id, 10))
		if reply.State == string(domain.AdminCancellationAccepted) {
			responseStatus = http.StatusAccepted
		}
	}
	r.strategyStatusReply(request, responseStatus, reply, err)
}

func (r *Routes) getOrderCancellation(ctx context.Context, request *app.RequestContext) {
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
	reply, err := r.cancellationQueries.GetOrderCancellation(call, &strategyv1.GetOrderCancellationRequest{
		Session:        session,
		StrategyId:     strategy,
		CancellationId: id,
	})
	if err == nil && !strategywire.ValidCancellationReceipt(reply, strategy, id) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func cancellationPathID(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}

func decodeCancellationInput(body []byte) (*strategyv1.OrderCancellationInput, error) {
	if len(body) == 0 || len(body) > maximumControlBytes || !utf8.Valid(body) {
		return nil, domain.ErrInvalidAdminCancellation
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, domain.ErrInvalidAdminCancellation
	}
	var revision, reason string
	seenRevision, seenReason := false, false
	for decoder.More() {
		key, keyErr := decoder.Token()
		value, valueErr := decoder.Token()
		text, valid := value.(string)
		if keyErr != nil || valueErr != nil || !valid {
			return nil, domain.ErrInvalidAdminCancellation
		}
		switch key {
		case "expected_order_revision":
			if seenRevision {
				return nil, domain.ErrInvalidAdminCancellation
			}
			revision, seenRevision = text, true
		case "reason":
			if seenReason {
				return nil, domain.ErrInvalidAdminCancellation
			}
			reason, seenReason = text, true
		default:
			return nil, domain.ErrInvalidAdminCancellation
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !seenRevision || !seenReason {
		return nil, domain.ErrInvalidAdminCancellation
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, domain.ErrInvalidAdminCancellation
	}
	id, valid := cancellationPathID(revision)
	if !valid {
		return nil, domain.ErrInvalidAdminCancellation
	}
	return &strategyv1.OrderCancellationInput{
		ExpectedOrderRevision: id,
		Reason:                reason,
	}, nil
}
