/*
Sponge is a caching, state-aware proxy library.

Sponge sits between clients and API services and acts very much like
If-Modified-Since, but doesn't waste time on headers, because most API clients
don't.

It just repeats the result from a cached API request, then starts a background
poll which hits it over a configurable amount of time, on a configurable
frequency, and waits for a state change. If a state change occurs, backend
polling stops, the cache is updated with the new result, and the rest of the
time is spent just serving from cache.

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

type SpongeHandler struct {
	TickTime             time.Duration
	TickCount            int64
	Proxy                SpongeProxy
	CacheExtraExpiration time.Duration
	CacheRunExpiration   time.Duration

	cache        map[string]SpongeProxyResult
	cache_expire map[string]time.Time
	mutex        sync.Mutex
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
