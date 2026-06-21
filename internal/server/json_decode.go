package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const (
	smallJSONBodyLimit   = 8 << 10
	defaultJSONBodyLimit = 64 << 10
)

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) error {
	if limit <= 0 {
		limit = defaultJSONBodyLimit
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON value")
	}
	return nil
}
