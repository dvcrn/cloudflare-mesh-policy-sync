//go:build !js

package main

import (
	"context"
	"flag"
	"log"
	"os"
)

func main() {
	dryRun := flag.Bool("n", false, "log planned changes without applying them")
	flag.Parse()
	s, err := newSyncer(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	s.DryRun = *dryRun
	if err := s.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
