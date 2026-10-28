package adminhttp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const maximumLoginBytes = 2048

type loginRequest struct {
	Username string
	Password string
}

var errInvalidLogin = errors.New("invalid login request")

// decodeLogin parses this fixed wire boundary directly into typed strings.
// A token reader rejects duplicate/unknown fields and non-string/null values.
func decodeLogin(body []byte) (loginRequest, error) {
	var result loginRequest
	if len(body) == 0 || len(body) > maximumLoginBytes || !utf8.Valid(body) {
		return result, errInvalidLogin
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return result, errInvalidLogin
	}
	seenUser, seenPassword := false, false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return result, errInvalidLogin
		}
		value, err := decoder.Token()
		text, valid := value.(string)
		if err != nil || !valid {
			return result, errInvalidLogin
		}
		switch key {
		case "username":
			if seenUser || len(text) < 3 || len(text) > 64 {
				return result, errInvalidLogin
			}
			seenUser, result.Username = true, text
		case "password":
			if seenPassword || len(text) < 12 || len(text) > 72 {
				return result, errInvalidLogin
			}
			seenPassword, result.Password = true, text
		default:
			return result, errInvalidLogin
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !seenUser || !seenPassword {
		return result, errInvalidLogin
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return result, errInvalidLogin
	}
	return result, nil
}
