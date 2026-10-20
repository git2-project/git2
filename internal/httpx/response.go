// Package httpx holds the JSON response helpers every service handler uses.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// maxBodyBytes caps every JSON request body. The auth payloads are a few
// hundred bytes; anything near this limit is not a real client.
const maxBodyBytes = 64 << 10

type errorBody struct {
	Error string `json:"error"`
}

func JSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("encode json response", "err", err)
	}
}

func Error(w http.ResponseWriter, status int, message string) {
	JSON(w, status, errorBody{Error: message})
}

// DecodeJSON decodes the request body into dst. It rejects unknown fields (a
// misspelled key fails loudly instead of being dropped), bodies over
// maxBodyBytes, and anything after the first JSON value.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must be a single JSON object")
	}
	return nil
}
