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

const maximumPublicationBytes = 8192

func (r *Routes) publishConfiguration(ctx context.Context, request *app.RequestContext) {
	if !r.sameOrigin(request) {
		r.failure(request, domain.ErrForbidden)
		return
	}
	id, validID := strategyID(request)
	input, validInput := decodePublication(request.Request.Body())
	keys := request.Request.Header.GetAll(IdempotencyHeader)
	if !validID || !validInput || !loginContentType(request) || len(request.URI().QueryString()) != 0 || len(keys) != 1 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	command, err := strategywire.Publication(input, id, keys[0])
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
	reply, err := r.publication.PublishConfiguration(call, &strategyv1.PublishConfigurationRequest{
		Session:        session,
		CsrfToken:      csrf[0],
		IdempotencyKey: command.Key,
		StrategyId:     command.StrategyID,
		Input:          input,
	})
	if err == nil && (reply == nil || reply.Id <= 0 || reply.ActorUserId <= 0 || reply.StrategyId != id || reply.CreatedAtMs <= 0 ||
		reply.PreviousRevision != command.ExpectedRevision || reply.Revision != command.ExpectedRevision+1 ||
		reply.PreviousConfigVersion != command.ExpectedConfigVersion || reply.ConfigVersion <= command.ExpectedConfigVersion ||
		reply.MarketDataSource != string(command.Source) || reply.Reason != command.Reason) {
		err = domain.ErrAdminControlUnavailable
	}
	if err == nil {
		request.Response.Header.Set("Location", "/api/v1/strategies/"+strconv.FormatInt(id, 10))
	}
	r.strategyStatusReply(request, http.StatusCreated, reply, err)
}

func decodePublication(body []byte) (*strategyv1.PublishConfigurationInput, bool) {
	if len(body) == 0 || len(body) > maximumPublicationBytes || !utf8.Valid(body) {
		return nil, false
	}
	var identity struct {
		Revision string `json:"expected_revision"`
		Version  string `json:"expected_config_version"`
	}
	if json.Unmarshal(body, &identity) != nil {
		return nil, false
	}
	revision, err := strconv.ParseInt(identity.Revision, 10, 64)
	if err != nil || revision <= 0 || strconv.FormatInt(revision, 10) != identity.Revision {
		return nil, false
	}
	version, err := strconv.ParseInt(identity.Version, 10, 64)
	if err != nil || version <= 0 || strconv.FormatInt(version, 10) != identity.Version {
		return nil, false
	}
	var input strategyv1.PublishConfigurationInput
	if protojson.Unmarshal(body, &input) != nil || input.ExpectedRevision != revision || input.ExpectedConfigVersion != version {
		return nil, false
	}
	return &input, true
}
