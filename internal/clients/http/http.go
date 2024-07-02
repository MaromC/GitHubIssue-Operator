package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
)

var (
	accept           = "Accept"
	acceptValue      = "application/vnd.github.v3+json"
	contentType      = "Content-Type"
	contentTypeValue = "application/json"
)

type HttpClient struct {
	Client *http.Client
}

// SendRequest sends a request to github.
func (r *HttpClient) SendRequest(url string, method string, body interface{}, logger logr.Logger) (*http.Response, error) {

	requestBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url, strings.NewReader(string(requestBody)))
	if err != nil {
		return nil, err
	}
	req.Header.Set(accept, acceptValue)
	req.Header.Set(contentType, contentTypeValue)

	response, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() {
		err := response.Body.Close()
		if err != nil {
			logger.Error(err, "failed closing response body")
		}
	}()

	return response, nil
}
