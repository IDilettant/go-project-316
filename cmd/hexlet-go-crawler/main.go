package main

import (
	"log"
	"net/http"
	"os"

	"code/cmd/hexlet-go-crawler/app"
	"code/internal/limiter"
)

func main() {
	httpClient := &http.Client{}

	clock := limiter.NewClock()

	err := app.Run(os.Args, os.Stdout, os.Stderr, httpClient, clock)
	if err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
