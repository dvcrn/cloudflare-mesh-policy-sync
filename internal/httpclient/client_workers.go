//go:build js && wasm

package httpclient

import (
	"net/http"

	"github.com/syumai/workers-go/cloudflare/fetch"
)

func newClient() *http.Client {
	return fetch.NewClient().HTTPClient(fetch.RedirectModeFollow)
}
