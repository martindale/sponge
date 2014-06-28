package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"sync"
	"syscall"
	"time"
)

type MyHandler struct {
	Result bool
	mutex  sync.Mutex
}

func (h *MyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body.Close()
	w.Header().Add("Content-Type", "application/json")
	b, _ := json.Marshal(h)
	w.Write(b)
	log.Printf("handled request: %b", h.Result)
	n, _ := rand.Int(rand.Reader, big.NewInt(1))
	if n.Bit(0) == 0 {
		h.mutex.Lock()
		h.Result = !h.Result
		h.mutex.Unlock()
	}
}

func main() {
	err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &syscall.Rlimit{Cur: 150000, Max: 150000})

	if err != nil {
		fmt.Println(err)
	}

	s := &http.Server{
		Addr:           ":8080",
		Handler:        &MyHandler{},
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 2048,
	}

	err = s.ListenAndServe()

	if err != nil {
		fmt.Println(err)
	}
}
