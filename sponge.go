package sponge

import (
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type SpongeHandler struct {
	TickTime             time.Duration
	TickCount            int
	BackendURL           url.URL
	Proxy                SpongeProxy
	CacheExtraExpiration time.Duration
	CacheRunExpiration   time.Duration

	cache        map[string]SpongeProxyResult
	cache_expire map[string]time.Time
	mutex        sync.Mutex
}

type SpongeProxyResult interface {
	SpongeProxyWriter
}

type SpongeProxyWriter interface {
	WriteToHTTP(w *http.ResponseWriter) error
}

type SpongeProxy interface {
	http.Handler

	MakeCacheKey(request *http.Request) (key string)
	MakeBackendRequest(request *http.Request) (result SpongeProxyResult, err error)
	HandleError(err error, writer *http.ResponseWriter)
}

func (sh *SpongeHandler) Init(cache map[string]SpongeProxyResult) {
	if cache == nil {
		sh.cache = make(map[string]SpongeProxyResult)
	} else {
		sh.cache = cache
	}

	go sh.do_cache_expiry()
}

func (sh *SpongeHandler) do_cache_expiry() {
	for {
		for key, value := range sh.cache_expire {
			if value.Add(-sh.CacheExtraExpiration).Before(time.Now().Add(sh.CacheExtraExpiration + (time.Duration(sh.TickCount * int(sh.TickTime))))) {
				delete(sh.cache_expire, key)
				delete(sh.cache, key)
			}
		}

		time.Sleep(sh.CacheRunExpiration)
	}
}

func (sh *SpongeHandler) check_tick(key string, request *http.Request) {
	tick := time.NewTicker(sh.TickTime)

	for tick_count := 0; tick_count < sh.TickCount; tick_count++ {
		<-tick.C

		result, err := sh.Proxy.MakeBackendRequest(request)

		if err != nil {
			log.Println("error:", err)
			continue
		}

		if sh.cache[key] != result {
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

		if err == nil {
			sh.Proxy.HandleError(err, &writer)
			sh.mutex.Unlock()
			return
		} else {
			sh.SetCache(request, result)
			value, _ = sh.cache[key]
			go sh.check_tick(key, request)
		}
	}

	sh.mutex.Unlock()
	value.WriteToHTTP(&writer)
}
