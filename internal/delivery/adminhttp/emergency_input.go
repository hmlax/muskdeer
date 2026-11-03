package adminhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	strategyv1 "github.com/hmlax/muskdeer/deer/proto/rpc/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func decodeEmergencyInput(body []byte) (*strategyv1.EmergencyCancellationInput, error) {
	if len(body) == 0 || len(body) > maximumControlBytes || !utf8.Valid(body) {
		return nil, domain.ErrInvalidAdminEmergencyCancel
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, domain.ErrInvalidAdminEmergencyCancel
	}
	var scope, revision, reason string
	seenScope, seenRevision, seenReason := false, false, false
	for decoder.More() {
		key, keyErr := decoder.Token()
		value, valueErr := decoder.Token()
		text, valid := value.(string)
		if keyErr != nil || valueErr != nil || !valid {
			return nil, domain.ErrInvalidAdminEmergencyCancel
		}
		switch key {
		case "scope":
			if seenScope {
				return nil, domain.ErrInvalidAdminEmergencyCancel
			}
			scope, seenScope = text, true
		case "expected_risk_revision":
			if seenRevision {
				return nil, domain.ErrInvalidAdminEmergencyCancel
			}
			revision, seenRevision = text, true
		case "reason":
			if seenReason {
				return nil, domain.ErrInvalidAdminEmergencyCancel
			}
			reason, seenReason = text, true
		default:
			return nil, domain.ErrInvalidAdminEmergencyCancel
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !seenScope || !seenRevision || !seenReason {
		return nil, domain.ErrInvalidAdminEmergencyCancel
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, domain.ErrInvalidAdminEmergencyCancel
	}
	id, valid := cancellationPathID(revision)
	if !valid {
		return nil, domain.ErrInvalidAdminEmergencyCancel
	}
	return &strategyv1.EmergencyCancellationInput{
		Scope:                scope,
		ExpectedRiskRevision: id,
		Reason:               reason,
	}, nil
}
