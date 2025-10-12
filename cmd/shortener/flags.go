package main

import (
    "flag"
)

var flagRunAddr string
var baseShortURL string

func parseFlags() {
    flag.StringVar(&flagRunAddr, "a", ":8080", "address and port to run server")
	flag.StringVar(&baseShortURL, "b", "http://localhost:8080", "base address before short url")
    flag.Parse()
}
