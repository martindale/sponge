package main

import (
	"encoding/json"
	sponge "github.com/erikh/sponge"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

type MyProxy struct{}

type MyResult struct {
	Result bool
}

func (proxy MyProxy) MakeCacheKey(request *http.Request) (key string) {
	return request.URL.Path
}

func (proxy MyProxy) MakeBackendRequest(request *http.Request) (sponge.SpongeProxyResult, error) {
	http_res, http_err := http.Get("http://localhost:8080/")

	if http_err != nil {
		return MyResult{}, http_err
	}

	defer http_res.Body.Close()
	b, io_err := ioutil.ReadAll(http_res.Body)

	if io_err != nil {
		return MyResult{}, io_err
	}

	log.Println(string(b))

	result := MyResult{}
	err := json.Unmarshal(b, &result)

	if err != nil {
		return MyResult{}, err
	}

	return result, nil
}

func (proxy MyProxy) HandleError(err error, writer http.ResponseWriter) {
	log.Println(err)
	writer.WriteHeader(500)
}

func (result MyResult) WriteToHTTP(writer http.ResponseWriter) error {
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}

	writer.Header().Add("Content-Type", "application/json")
	writer.Write(b)

	return nil
}

func main() {
	sh := &sponge.SpongeHandler{
		TickTime:             1 * time.Second,
		TickCount:            10,
		Proxy:                MyProxy{},
		CacheExtraExpiration: 0 * time.Second,
		CacheRunExpiration:   1 * time.Second,
	}

	sh.Init(nil)

	s := &http.Server{
		Addr:           ":8081",
		Handler:        sh,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 2048,
	}

	s.ListenAndServe()
}
