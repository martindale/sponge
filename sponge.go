/*
Sponge is a caching, state-aware proxy library.

Sponge sits between clients and API services and acts very much like
If-Modified-Since, but doesn't waste time on cache headers, because most API
clients don't.

It just repeats and caches the result from an API request, then starts a
background poll which hits it over a configurable amount of time, on a
configurable frequency, and waits for a state change. If a state change occurs,
backend polling stops, the cache is updated with the new result, and the rest
of the time is spent just serving from cache. After cache expiration, the
process would repeat itself for a given API request.

Again, this is a library -- you are expected to implement your own services
code, as well as code to generate other functions of the proxy. There is an
example of a basic service in the examples/ directory.
*/
package sponge

import (
	"log"
	"net/http"
	"sync"
	"time"
)

/*
This is intended to be consumed by http.Server and similar tooling.

Example:

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
*/
type SpongeHandler struct {
	// Time between each check against the backend.
	TickTime time.Duration

	// Number of times to check the backend before stopping the background
	// checking.
	TickCount int64

	// An object that implements SpongeProxy.
	Proxy SpongeProxy

	// Cache Expiration is determined by:
	// (TickTime * TickCount) + CacheExtraExpiration
	CacheExtraExpiration time.Duration

	// How frequently to check the cache.
	CacheRunExpiration time.Duration

	cache        map[string]SpongeProxyResult // The actual cache
	cache_expire map[string]time.Time         // expire management
	mutex        sync.Mutex                   // mutex for first-hit synchronization
}

type SpongeProxyResult interface {
	SpongeProxyWriter
	Equal(other SpongeProxyResult) bool
}

type SpongeProxyWriter interface {
	WriteToHTTP(w http.ResponseWriter) error
}

type SpongeProxy interface {
	MakeCacheKey(request *http.Request) (key string)
	MakeBackendRequest(request *http.Request) (result SpongeProxyResult, err error)
	HandleError(err error, writer http.ResponseWriter)
}

func (sh *SpongeHandler) Init(cache map[string]SpongeProxyResult) {
	if cache == nil {
		sh.cache = make(map[string]SpongeProxyResult)
	} else {
		sh.cache = cache
	}

	sh.cache_expire = make(map[string]time.Time)

	go sh.do_cache_expiry()
}

func (sh *SpongeHandler) do_cache_expiry() {
	expiration_time := sh.CacheExtraExpiration + (time.Duration(sh.TickCount * int64(sh.TickTime)))

	for {
		for key, value := range sh.cache_expire {
			if time.Now().Add(-expiration_time).After(value.Add(sh.CacheExtraExpiration)) {
				delete(sh.cache_expire, key)
				delete(sh.cache, key)
			}
		}

		time.Sleep(sh.CacheRunExpiration)
	}
}

func (sh *SpongeHandler) check_tick(key string, request *http.Request) {
	tick := time.NewTicker(sh.TickTime)

	for tick_count := 0; int64(tick_count) < sh.TickCount; tick_count++ {
		<-tick.C

		result, err := sh.Proxy.MakeBackendRequest(request)

		if err != nil {
			log.Println("error:", err)
			continue
		}

		if !sh.cache[key].Equal(result) {
			sh.SetCache(request, result)
			tick.Stop()
			return
		}
	}
}

func (sh *SpongeHandler) SetCache(request *http.Request, value SpongeProxyResult) {
	key := sh.Proxy.MakeCacheKey(request)
	sh.cache[key] = value
	sh.cache_expire[key] = time.Now()
}

func (sh *SpongeHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	key := sh.Proxy.MakeCacheKey(request)

	sh.mutex.Lock()
	value, ok := sh.cache[key]

	if !ok {
		result, err := sh.Proxy.MakeBackendRequest(request)

		if err != nil {
			sh.Proxy.HandleError(err, writer)
			sh.mutex.Unlock()
			return
		} else {
			sh.SetCache(request, result)
			value, _ = sh.cache[key]
			go sh.check_tick(key, request)
		}
	}

	sh.mutex.Unlock()
	value.WriteToHTTP(writer)
}
