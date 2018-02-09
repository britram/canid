package canid

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Prefix information

type PrefixInfo struct {
	Prefix      string
	ASN         int
	CountryCode string
	Cached      time.Time
}

type PrefixCache struct {
	Data            map[string]PrefixInfo
	lock            sync.RWMutex
	expiry          int
	backend_limiter chan struct{}
}

func NewPrefixCache(expiry int, concurrency_limit int) *PrefixCache {
	c := new(PrefixCache)
	c.Data = make(map[string]PrefixInfo)
	c.expiry = expiry
	c.backend_limiter = make(chan struct{}, concurrency_limit)
	return c
}

func (cache *PrefixCache) Lookup(addr net.IP) (out PrefixInfo, err error) {
	// Determine starting prefix by guessing whether this is v6 or not
	var prefixlen, addrbits int
	if strings.Contains(addr.String(), ":") {
		prefixlen = 48
		addrbits = 128
	} else {
		prefixlen = 24
		addrbits = 32
	}

	// Iterate through prefixes looking for a match
	for i := prefixlen; i > 0; i-- {
		mask := net.CIDRMask(i, addrbits)
		net := net.IPNet{addr.Mask(mask), mask}
		prefix := net.String()

		cache.lock.RLock()
		out, ok := cache.Data[prefix]
		cache.lock.RUnlock()
		if ok {
			// check for expiry
			if int(time.Since(out.Cached).Seconds()) > cache.expiry {
				log.Printf("entry expired for prefix %s", prefix)
				cache.lock.Lock()
				delete(cache.Data, prefix)
				cache.lock.Unlock()
				break
			} else {
				log.Printf("cache hit! for prefix %s", prefix)
				return out, nil
			}
		}
	}

	// Cache miss, go ask RIPE
	cache.backend_limiter <- struct{}{}
	out, err = LookupRipestat(addr)
	_ = <-cache.backend_limiter
	if err != nil {
		return
	}

	// cache and return
	out.Cached = time.Now().UTC()
	cache.lock.Lock()
	cache.Data[out.Prefix] = out
	cache.lock.Unlock()
	log.Printf("cached prefix %s -> %v", out.Prefix, out)

	return
}

func (cache *PrefixCache) LookupServer(w http.ResponseWriter, req *http.Request) {

	ip := net.ParseIP(req.URL.Query().Get("addr"))
	if ip == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	prefix_info, err := cache.Lookup(ip)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError) // FIXME not always a 500
		error_struct := struct{ Error string }{err.Error()}
		error_body, _ := json.Marshal(error_struct)
		w.Write(error_body)
		return
	}

	prefix_body, _ := json.Marshal(prefix_info)
	w.Write(prefix_body)
}
