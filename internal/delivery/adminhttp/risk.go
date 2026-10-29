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

func (r *Routes) getStrategyRisk(ctx context.Context, request *app.RequestContext) {
	id, valid := strategyID(request)
	if !valid || len(request.URI().QueryString()) != 0 || len(request.Request.Body()) != 0 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.riskQueries.GetStrategyRisk(call, &strategyv1.GetStrategyRiskRequest{
		Session:    session,
		StrategyId: id,
	})
	if err == nil && !strategywire.ValidRiskState(reply, id) {
		err = status.Error(codes.Unavailable, domain.MessageAdminUnavailable)
	}
	r.strategyReply(request, reply, err)
}

func (r *Routes) changeStrategyRisk(ctx context.Context, request *app.RequestContext) {
	if !r.sameOrigin(request) {
		r.failure(request, domain.ErrForbidden)
		return
	}
	id, valid := strategyID(request)
	input, err := decodeRiskInput(request.Request.Body())
	keys := request.Request.Header.GetAll(IdempotencyHeader)
	if !valid || err != nil || !loginContentType(request) || len(request.URI().QueryString()) != 0 || len(keys) != 1 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	command, err := strategywire.RiskCommand(input, id, keys[0])
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
	reply, err := r.riskControls.ChangeStrategyRisk(call, &strategyv1.ChangeStrategyRiskRequest{
		Session:        session,
		CsrfToken:      csrf[0],
		IdempotencyKey: command.Key,
		StrategyId:     id,
		Input:          input,
	})
	if err == nil && !strategywire.ValidRiskReceipt(reply, command) {
		err = status.Error(codes.Unavailable, domain.MessageAdminControlUnavailable)
	}
	if status.Code(err) == codes.FailedPrecondition {
		writeError(request, http.StatusConflict, domain.AdminRiskRecoveryDeniedCode, domain.MessageAdminRiskRecoveryDenied)
		return
	}
	if err == nil {
		request.Response.Header.Set("Location", "/api/v1/strategies/"+strconv.FormatInt(id, 10)+"/risk")
	}
	r.strategyReply(request, reply, err)
}

type riskInputText struct {
	action       string
	revision     string
	reason       string
	config       string
	desired      string
	seenAction   bool
	seenRevision bool
	seenReason   bool
	seenConfig   bool
	seenDesired  bool
}

func decodeRiskInput(body []byte) (*strategyv1.ChangeStrategyRiskInput, error) {
	if len(body) == 0 || len(body) > maximumControlBytes || !utf8.Valid(body) {
		return nil, domain.ErrInvalidAdminRisk
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, domain.ErrInvalidAdminRisk
	}
	var text riskInputText
	for decoder.More() {
		key, keyErr := decoder.Token()
		value, valueErr := decoder.Token()
		field, fieldOK := key.(string)
		content, contentOK := value.(string)
		if keyErr != nil || valueErr != nil || !fieldOK || !contentOK || !text.assign(field, content) {
			return nil, domain.ErrInvalidAdminRisk
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !text.seenAction || !text.seenRevision || !text.seenReason {
		return nil, domain.ErrInvalidAdminRisk
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, domain.ErrInvalidAdminRisk
	}
	return text.input()
}

func (r *riskInputText) assign(key, value string) bool {
	for _, field := range []struct {
		name  string
		value *string
		seen  *bool
	}{
		{
			name:  "action",
			value: &r.action,
			seen:  &r.seenAction,
		},
		{
			name:  "expected_revision",
			value: &r.revision,
			seen:  &r.seenRevision,
		},
		{
			name:  "reason",
			value: &r.reason,
			seen:  &r.seenReason,
		},
		{
			name:  "expected_config_version",
			value: &r.config,
			seen:  &r.seenConfig,
		},
		{
			name:  "expected_desired_state",
			value: &r.desired,
			seen:  &r.seenDesired,
		},
	} {
		if field.name != key {
			continue
		}
		if *field.seen {
			return false
		}
		*field.value, *field.seen = value, true
		return true
	}
	return false
}

func (r riskInputText) input() (*strategyv1.ChangeStrategyRiskInput, error) {
	revision, err := riskPositiveID(r.revision)
	if err != nil {
		return nil, err
	}
	var version int64
	if domain.AdminRiskAction(r.action) == domain.AdminRiskBlock {
		if r.seenConfig || r.seenDesired {
			return nil, domain.ErrInvalidAdminRisk
		}
	} else {
		if !r.seenConfig || !r.seenDesired {
			return nil, domain.ErrInvalidAdminRisk
		}
		version, err = riskPositiveID(r.config)
		if err != nil {
			return nil, err
		}
	}
	return &strategyv1.ChangeStrategyRiskInput{
		Action:                r.action,
		ExpectedRevision:      revision,
		Reason:                r.reason,
		ExpectedConfigVersion: version,
		ExpectedDesiredState:  r.desired,
	}, nil
}

func riskPositiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0, domain.ErrInvalidAdminRisk
	}
	return id, nil
}
