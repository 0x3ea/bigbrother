package main

import (
	"log"

	"github.com/0x3ea/bigbrother/internal/config"
)

func main() {
	targets, err := config.LoadTargets()
	if err != nil {
		log.Fatal(err)
	}
	for _, t := range targets {
		log.Printf("monitor targe: %s, porber every %ds", t.URL, t.IntervalS)
	}
}
