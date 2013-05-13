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

/*
This is a result interface that you implement, and corresponds to the kind of
results you'll see both from the backend and send to the requesting clients.

Examples of this in use are in examples/test_sponge.go.
*/
type SpongeProxyResult interface {
	SpongeProxyWriter
	// Is the receiver equal to this other object? Used in determining whether
	// to update the cache.
	Equal(other SpongeProxyResult) bool
}

/*
SpongeProxyWriter is just a composable interface for writing to http objects.
*/
type SpongeProxyWriter interface {
	// Write this response to a http.ResponseWriter.
	WriteToHTTP(w http.ResponseWriter) error
}

/*
SpongeProxy is the proxy logic itself, you implement this and assign it to a
SpongeHandler's Proxy member. It contains methods you implement for handling
cache lookups, errors, and making backend requests.

Examples of this in use are in examples/test_sponge.go.
*/
type SpongeProxy interface {
	// Make a cache key which coordinates with the SpongeProxyResult objects.
	MakeCacheKey(request *http.Request) (key string)
	// Make a backend request. The original request will be passed in to assist
	// with any forwarding that needs to be done to the backend, or in the
	// other direction, errors.
	MakeBackendRequest(request *http.Request) (result SpongeProxyResult, err error)
	// If an error is encountered while making the backend request, this method
	// will be called. The ResponseWriter is passed in so you can do things
	// like return a 500 status code, etc.
	HandleError(err error, writer http.ResponseWriter)
}

/*
Initialize a SpongeHandler. If the argument is nil, it will create the map it
needs for the cache. Otherwise, you can pass another cache in (for it to share,
or to restore a cache) and it will be used.
*/
func (sh *SpongeHandler) Init(cache map[string]SpongeProxyResult) {
	if cache == nil {
		sh.cache = make(map[string]SpongeProxyResult)
	} else {
		sh.cache = cache
	}

	sh.cache_expire = make(map[string]time.Time)

	go sh.do_cache_expiry()
}

/*
Run the cache expriation -- runs as a goroutine, similar to a Monitor.
*/
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

/*
This periodically hits the backend until something changes, or the number of
ticks has exhausted.
*/
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

/*
Function to update the cache and expiration at the same time.
*/
func (sh *SpongeHandler) SetCache(request *http.Request, value SpongeProxyResult) {
	key := sh.Proxy.MakeCacheKey(request)
	sh.cache[key] = value
	sh.cache_expire[key] = time.Now()
}

/*
http.Server handler -- actually responds to the request.
*/
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
		}

		sh.SetCache(request, result)
		value, _ = sh.cache[key]
		go sh.check_tick(key, request)
	}

	sh.mutex.Unlock()
	value.WriteToHTTP(writer)
}
