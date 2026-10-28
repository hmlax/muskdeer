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

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

// IdempotencyHeader permanently binds a management request to its original body.
const IdempotencyHeader = "Idempotency-Key"

const maximumControlBytes = 2048

type controlBody struct {
	Action           domain.AdminControlAction
	ExpectedRevision int64
	Reason           string
}

func (r *Routes) controlStrategy(ctx context.Context, request *app.RequestContext) {
	if !r.sameOrigin(request) {
		r.failure(request, domain.ErrForbidden)
		return
	}
	id, valid := strategyID(request)
	input, err := decodeControl(request.Request.Body())
	// GetAll owns its strings: later Content-Type/Cookie reads may reuse
	// Hertz's PeekAll buffer and must not overwrite the original request key.
	keys := request.Request.Header.GetAll(IdempotencyHeader)
	if !valid || !loginContentType(request) || len(request.URI().QueryString()) != 0 || err != nil || len(keys) != 1 {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	command, err := (domain.AdminControlRequest{
		Key:              keys[0],
		StrategyID:       id,
		ExpectedRevision: input.ExpectedRevision,
		Action:           input.Action,
		Reason:           input.Reason,
	}).Normalize()
	if err != nil {
		writeError(request, http.StatusBadRequest, codeInvalidRequest, messageInvalidRequest)
		return
	}
	session, valid := r.strategySession(request)
	if !valid {
		return
	}
	csrf := request.Request.Header.PeekAll(CSRFHeader)
	if len(csrf) != 1 || !cookieSecret(string(csrf[0])) {
		r.failure(request, domain.ErrForbidden)
		return
	}
	call, cancel := context.WithTimeout(ctx, strategyReadTimeout)
	defer cancel()
	reply, err := r.controls.ControlStrategy(call, &strategyv1.ControlStrategyRequest{
		Session:          session,
		CsrfToken:        string(csrf[0]),
		IdempotencyKey:   command.Key,
		StrategyId:       command.StrategyID,
		ExpectedRevision: command.ExpectedRevision,
		Action:           string(command.Action),
		Reason:           command.Reason,
	})
	if err == nil && (reply == nil || reply.Id <= 0 || reply.StrategyId != command.StrategyID || reply.ExpectedRevision != command.ExpectedRevision) {
		err = domain.ErrAdminControlUnavailable
	}
	responseStatus := http.StatusOK
	if err == nil && reply != nil && reply.Id > 0 {
		request.Response.Header.Set("Location", "/api/v1/operations/"+strconv.FormatInt(reply.Id, 10))
		if reply.State == string(domain.AdminOperationAccepted) {
			responseStatus = http.StatusAccepted
		}
	}
	r.strategyStatusReply(request, responseStatus, reply, err)
}

func (r *Routes) getOperation(ctx context.Context, request *app.RequestContext) {
	id, valid := operationID(request)
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
	reply, err := r.controls.GetOperation(call, &strategyv1.GetOperationRequest{
		Session:     session,
		OperationId: id,
	})
	r.strategyReply(request, reply, err)
}

func operationID(request *app.RequestContext) (int64, bool) {
	value := request.Param("operationID")
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0 && strconv.FormatInt(id, 10) == value
}

// decodeControl is the actual JSON wire boundary. All business values have
// explicit fields; duplicate/unknown names, null, numeric revisions and trailing
// data are rejected instead of being silently normalized by a generic decoder.
func decodeControl(body []byte) (controlBody, error) {
	var result controlBody
	if len(body) == 0 || len(body) > maximumControlBytes || !utf8.Valid(body) {
		return result, domain.ErrInvalidAdminControl
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return result, domain.ErrInvalidAdminControl
	}
	seenAction, seenRevision, seenReason := false, false, false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return result, domain.ErrInvalidAdminControl
		}
		value, err := decoder.Token()
		text, valid := value.(string)
		if err != nil || !valid {
			return result, domain.ErrInvalidAdminControl
		}
		switch key {
		case "action":
			if seenAction {
				return result, domain.ErrInvalidAdminControl
			}
			seenAction, result.Action = true, domain.AdminControlAction(text)
		case "expected_revision":
			if seenRevision {
				return result, domain.ErrInvalidAdminControl
			}
			revision, err := strconv.ParseInt(text, 10, 64)
			if err != nil || revision <= 0 || strconv.FormatInt(revision, 10) != text {
				return result, domain.ErrInvalidAdminControl
			}
			seenRevision, result.ExpectedRevision = true, revision
		case "reason":
			if seenReason {
				return result, domain.ErrInvalidAdminControl
			}
			seenReason, result.Reason = true, text
		default:
			return result, domain.ErrInvalidAdminControl
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !seenAction || !seenRevision || !seenReason {
		return result, domain.ErrInvalidAdminControl
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return result, domain.ErrInvalidAdminControl
	}
	return result, nil
}
