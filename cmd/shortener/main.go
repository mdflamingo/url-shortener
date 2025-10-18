package main

import (
	"github.com/mdflamingo/url-shortener/internal/config"
	"github.com/mdflamingo/url-shortener/internal/handler"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func main() {
	conf := config.ParseFlags()
	if err := run(conf); err != nil {
		log.Fatal(err)
	}
}

func run(conf *config.Config) error {
	log.Printf("Running server on %s\n", conf.FlagRunAddr)
	log.Printf("Base short URL: %s\n", conf.BaseShortURL)

	handler.Storage = make(map[string]string)
	r := chi.NewRouter()

	r.Get("/{id}", handler.GetHandler)
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		handler.PostHandler(w, req, conf.BaseShortURL)
	})

	return http.ListenAndServe(conf.FlagRunAddr, r)
}
