package sling

import (
	"encoding/json"
	"io"
	"net/http"
)

// ResponseDecoder decodes http responses into struct values.
type ResponseDecoder interface {
	// Decode decodes the response into the value pointed to by v.
	Decode(resp *http.Response, v interface{}) error
}

// jsonDecoder decodes http response JSON into a JSON-tagged struct value.
type jsonDecoder struct {
}

// Decode decodes the Response Body into the value pointed to by v.
// Caller must provide a non-nil v and close the resp.Body.
func (d jsonDecoder) Decode(resp *http.Response, v interface{}) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return newError(body, resp.StatusCode, err)
	}

	err = json.Unmarshal(body, v)
	if err != nil {
		return newError(body, resp.StatusCode, err)
	}

	return nil
}
