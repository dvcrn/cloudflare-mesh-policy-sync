package httpclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// New returns an outbound client backed by fetch on Workers and net/http elsewhere.
func New() *http.Client {
	client := newClient()
	client.Timeout = 10 * time.Second
	return client
}

// DoJSON sends req and decodes a 2xx JSON body into out, which may be nil.
func DoJSON(client *http.Client, req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s %s: %s: %s", req.Method, req.URL.Path, resp.Status, body)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
