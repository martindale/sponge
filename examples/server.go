package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type MyHandler struct {
	Result bool
}

func (h *MyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	b, _ := json.Marshal(h)
	w.Write(b)
	log.Printf("handled request: %b", h.Result)

	h.Result = !h.Result
}

func main() {
	s := &http.Server{
		Addr:           ":8080",
		Handler:        &MyHandler{},
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 2048,
	}

	s.ListenAndServe()
}
