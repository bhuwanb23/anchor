package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxBodySize = 1 << 20 // 1 MB

// DecodeJSON reads the request body (up to 1 MB), decodes JSON into dst,
// and rejects unknown fields. Returns a user-readable error on failure.
func DecodeJSON(r *http.Request, dst interface{}) error {
	defer r.Body.Close()

	limited := io.LimitReader(r.Body, maxBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("failed to read request body")
	}

	if len(body) == 0 {
		return fmt.Errorf("request body is empty")
	}
	if len(body) > maxBodySize {
		return fmt.Errorf("request body too large (max 1 MB)")
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}

	// Reject trailing data (e.g. "null null").
	var extra json.RawMessage
	if err := dec.Decode(&extra); err == nil {
		return fmt.Errorf("request body must contain a single JSON value")
	}

	return nil
}

// decodeError translates low-level JSON errors into human-readable messages.
func decodeError(err error) error {
	switch {
	case err == io.EOF:
		return fmt.Errorf("request body is empty")
	case err == io.ErrUnexpectedEOF:
		return fmt.Errorf("request body is incomplete")
	default:
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return fmt.Errorf("invalid JSON in request body")
		}

		var unmarshalErr *json.UnmarshalTypeError
		if errors.As(err, &unmarshalErr) {
			return fmt.Errorf("field %q must be a %s", unmarshalErr.Field, unmarshalErr.Type)
		}

		if strings.Contains(err.Error(), "unknown field") {
			msg := err.Error()
			// Go's error: `json: "unknown field foo"`
			if idx := strings.Index(msg, "unknown field "); idx > 0 {
				field := msg[idx+len("unknown field "):]
				field = strings.Trim(field, "\"")
				return fmt.Errorf("unknown field %q", field)
			}
			return fmt.Errorf("unknown field in request body")
		}

		return fmt.Errorf("invalid request body: %v", err)
	}
}
