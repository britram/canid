package canid

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type AddressInfo struct {
	Name      string
	Addresses []net.IP
	Cached    time.Time
}

type AddressCache struct {
	Data            map[string]AddressInfo
	lock            sync.RWMutex
	prefixes        *PrefixCache
	expiry          int
	backend_limiter chan struct{}
}

func NewAddressCache(expiry int, concurrency_limit int, prefixcache *PrefixCache) *AddressCache {
	c := new(AddressCache)
	c.Data = make(map[string]AddressInfo)
	c.expiry = expiry
	c.backend_limiter = make(chan struct{}, concurrency_limit)
	c.prefixes = prefixcache
	return c
}

func (cache *AddressCache) Lookup(name string) (out AddressInfo) {
	// Cache lookup
	var ok bool
	cache.lock.RLock()
	out, ok = cache.Data[name]
	cache.lock.RUnlock()
	if ok {
		// check for expiry
		if int(time.Since(out.Cached).Seconds()) > cache.expiry {
			log.Printf("entry expired for name %s", name)
			cache.lock.Lock()
			delete(cache.Data, name)
			cache.lock.Unlock()
		} else {
			log.Printf("cache hit for name %s", name)
			return
		}
	}

	// Cache miss. Lookup.
	out.Name = name
	cache.backend_limiter <- struct{}{}
	addrs, err := net.LookupIP(name)
	_ = <-cache.backend_limiter
	if err == nil {
		// we have addresses. precache prefix information.
		out.Addresses = addrs
		// precache prefixes, ignoring results
		if cache.prefixes != nil {
			for _, addr := range addrs {
				_, _ = cache.prefixes.Lookup(addr)
			}
		}
	} else {
		out.Addresses = make([]net.IP, 0)
		log.Printf("error looking up %s: %s", name, err.Error())
		err = nil
	}

	// cache and return
	out.Cached = time.Now().UTC()
	cache.lock.Lock()
	cache.Data[out.Name] = out
	cache.lock.Unlock()
	log.Printf("cached name %s -> %v", out.Name, out)
	return
}

func (cache *AddressCache) LookupServer(w http.ResponseWriter, req *http.Request) {
	// TODO figure out how to duplicate less code here
	name := req.URL.Query().Get("name")
	if len(name) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	addr_info := cache.Lookup(name)

	addr_body, _ := json.Marshal(addr_info)
	w.Write(addr_body)
}
