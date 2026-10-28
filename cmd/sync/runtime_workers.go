//go:build js && wasm

package main

import (
	"context"

	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/cron"
)

func main() {
	cron.ScheduleTask(func(ctx context.Context) error {
		s, err := newSyncer(cloudflare.Getenv)
		if err != nil {
			return err
		}
		return s.Run(ctx)
	})
}
