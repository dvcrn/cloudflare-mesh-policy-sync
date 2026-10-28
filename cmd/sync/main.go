package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/dvcrn/cloudflare-mesh-dns-sync/internal/cloudflare"
	"github.com/dvcrn/cloudflare-mesh-dns-sync/internal/httpclient"
	"github.com/dvcrn/cloudflare-mesh-dns-sync/internal/reconcile"
)

func newSyncer(getenv func(string) string) (*reconcile.Syncer, error) {
	env := map[string]string{}
	for _, k := range []string{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID", "DOMAIN", "ALLOWED_IDENTITIES"} {
		if env[k] = getenv(k); env[k] == "" {
			return nil, fmt.Errorf("%s is not set", k)
		}
	}
	var allowed []string
	for _, e := range strings.Split(env["ALLOWED_IDENTITIES"], ",") {
		if e = strings.TrimSpace(e); e != "" {
			allowed = append(allowed, e)
		}
	}
	staleAfter := 168 * time.Hour
	if v := getenv("STALE_AFTER"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("STALE_AFTER: %w", err)
		}
		staleAfter = d
	}
	return &reconcile.Syncer{
		Cloudflare: &cloudflare.Client{
			HTTP:      httpclient.New(),
			APIToken:  env["CLOUDFLARE_API_TOKEN"],
			AccountID: env["CLOUDFLARE_ACCOUNT_ID"],
		},
		Domain:     env["DOMAIN"],
		Allowed:    allowed,
		StaleAfter: staleAfter,
	}, nil
}
