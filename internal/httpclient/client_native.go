//go:build !js

package httpclient

import "net/http"

func newClient() *http.Client {
	return &http.Client{Transport: http.DefaultTransport}
}
