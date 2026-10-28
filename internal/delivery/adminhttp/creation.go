package adminhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/cloudwego/hertz/pkg/app"
	"google.golang.org/protobuf/encoding/protojson"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

const maximumCreationBytes = 8192

func (r *Routes) createStrategy(ctx context.Context, request *app.RequestContext) {
	if !r.sameOrigin(request) {
		r.failure(request, domain.ErrForbidden)
		return
	}
	input, valid := decodeCreation(request.Request.Body())
	keys := request.Request.Header.GetAll(IdempotencyHeader)
	if !valid || !loginContentType(request) || len(request.URI().QueryString()) != 0 || len(keys) != 1 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	command, err := strategywire.Creation(input, keys[0])
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
	reply, err := r.creation.CreateStrategy(call, &strategyv1.CreateStrategyRequest{
		Session:        session,
		CsrfToken:      csrf[0],
		IdempotencyKey: command.Key,
		Input:          input,
	})
	if err == nil && (reply == nil || reply.Id <= 0 || reply.StrategyId <= 0 || reply.Revision != 1 || reply.ConfigVersion != 1 || reply.MarketDataSource != input.MarketDataSource || reply.Reason != command.Reason) {
		err = domain.ErrAdminControlUnavailable
	}
	if err == nil {
		request.Response.Header.Set("Location", "/api/v1/strategies/"+strconv.FormatInt(reply.StrategyId, 10))
	}
	r.strategyStatusReply(request, http.StatusCreated, reply, err)
}

func decodeCreation(body []byte) (*strategyv1.CreateStrategyInput, bool) {
	if len(body) == 0 || len(body) > maximumCreationBytes || !utf8.Valid(body) {
		return nil, false
	}
	// ProtoJSON validates the full concrete input, including duplicate/unknown
	// nested fields. This independent wire-kind check additionally requires the
	// long identity to be a canonical JSON string, not ProtoJSON's number alias.
	var identity struct {
		InstrumentID string `json:"instrument_id"`
	}
	if json.Unmarshal(body, &identity) != nil {
		return nil, false
	}
	id, err := strconv.ParseInt(identity.InstrumentID, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != identity.InstrumentID {
		return nil, false
	}
	var input strategyv1.CreateStrategyInput
	if protojson.Unmarshal(body, &input) != nil || input.InstrumentId != id {
		return nil, false
	}
	return &input, true
}
