package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
)

// Structure partially covering the output of RIPEstat's prefix overview and
// geolocation API calls, for decoding JSON reponses from RIPEstat.

type RipeStatResponse struct {
	Status string
	Data   struct {
		Resource string
		ASNs     []struct {
			ASN int
		}
		Locations []struct {
			Country string
		}
	}
}

// Prefix information

type PrefixInfo struct {
	Prefix      string
	ASN         int
	CountryCode string
	Cached      time.Time
}

// RIPEstat backend

const ripeStatPrefixURL = "https://stat.ripe.net/data/prefix-overview/data.json"
const ripeStatGeolocURL = "https://stat.ripe.net/data/geoloc/data.json"

func callRipestat(apiurl string, addr net.IP, out *PrefixInfo) error {

	// construct a query string and add it to the URL
	v := make(url.Values)
	v.Add("resource", addr.String())
	fullUrl, err := url.Parse(apiurl)
	if err != nil {
		return err
	}
	fullUrl.RawQuery = v.Encode()

	log.Printf("calling ripestat %s", fullUrl.String())

	resp, err := http.Get(fullUrl.String())
	if err != nil {
		return err
	}

	// and now we have a response, parse it
	var doc RipeStatResponse
	dec := json.NewDecoder(resp.Body)
	err = dec.Decode(&doc)
	if err != nil {
		return err
	}

	// don't even bother if the server told us to go away
	if doc.Status != "ok" {
		return errors.New("RIPEstat request failed with status " + doc.Status)
	}

	// store the prefix, if not already present
	if len(out.Prefix) == 0 {
		out.Prefix = doc.Data.Resource
	}

	// get the first AS number, if present
	for _, asn := range doc.Data.ASNs {
		out.ASN = asn.ASN
		break
	}

	// get the first country code, if present
	for _, location := range doc.Data.Locations {
		out.CountryCode = location.Country
		break
	}

	return nil
}

func lookupRipestat(addr net.IP) (out PrefixInfo, err error) {
	err = callRipestat(ripeStatPrefixURL, addr, &out)
	if err == nil {
		callRipestat(ripeStatGeolocURL, addr, &out)
	}
	return
}

// Map of prefixes to information about them, stored by prefix.

type PrefixCache struct {
	Data            map[string]PrefixInfo
	lock            sync.RWMutex
	expiry          int
	backend_limiter chan struct{}
}

func (cache *PrefixCache) lookup(addr net.IP) (out PrefixInfo, err error) {
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
	out, err = lookupRipestat(addr)
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

func (cache *PrefixCache) lookupServer(w http.ResponseWriter, req *http.Request) {

	ip := net.ParseIP(req.URL.Query().Get("addr"))
	if ip == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	prefix_info, err := cache.lookup(ip)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		error_struct := struct{ Error string }{err.Error()}
		error_body, _ := json.Marshal(error_struct)
		w.Write(error_body)
		return
	}

	prefix_body, _ := json.Marshal(prefix_info)
	w.Write(prefix_body)
}

// Map of names to addresses

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

func (cache *AddressCache) lookup(name string) (out AddressInfo) {
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
		for _, addr := range addrs {
			_, _ = cache.prefixes.lookup(addr)
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

func (cache *AddressCache) lookupServer(w http.ResponseWriter, req *http.Request) {
	// TODO figure out how to duplicate less code here
	name := req.URL.Query().Get("name")
	if len(name) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	addr_info := cache.lookup(name)
	// if err != nil {
	// 	w.WriteHeader(http.StatusInternalServerError)
	// 	error_struct := struct{ Error string }{err.Error()}
	// 	error_body, _ := json.Marshal(error_struct)
	// 	w.Write(error_body)
	// 	return
	// }

	addr_body, _ := json.Marshal(addr_info)
	w.Write(addr_body)
}

const CANID_STORAGE_VERSION = 1

type CanidStorage struct {
	Version   int
	Prefixes  *PrefixCache
	Addresses *AddressCache
}

func (storage *CanidStorage) undump(in io.Reader) error {
	dec := json.NewDecoder(in)
	return dec.Decode(storage)
}

func (storage *CanidStorage) dump(out io.Writer) error {
	enc := json.NewEncoder(out)
	return enc.Encode(*storage)
}

func newStorage(expiry int, limit int) *CanidStorage {
	storage := new(CanidStorage)
	storage.Version = CANID_STORAGE_VERSION
	storage.Prefixes = new(PrefixCache)
	storage.Prefixes.Data = make(map[string]PrefixInfo)
	storage.Prefixes.expiry = expiry
	storage.Prefixes.backend_limiter = make(chan struct{}, limit)
	storage.Addresses = new(AddressCache)
	storage.Addresses.Data = make(map[string]AddressInfo)
	storage.Addresses.prefixes = storage.Prefixes
	storage.Addresses.expiry = expiry
	storage.Addresses.backend_limiter = make(chan struct{}, limit)
	return storage
}

func main() {
	fileflag := flag.String("file", "", "backing store for caches (JSON file)")
	expiryflag := flag.Int("expiry", 600, "expire cache entries after n sec")
	limitflag := flag.Int("concurrency", 16, "simultaneous backend request limit")

	// parse command line
	flag.Parse()

	// set up sigterm handling
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// allocate and link cache
	storage := newStorage(*expiryflag, *limitflag)

	// undump cache if filename given
	if len(*fileflag) > 0 {
		infile, ferr := os.Open(*fileflag)
		if ferr == nil {
			cerr := storage.undump(infile)
			infile.Close()
			if cerr != nil {
				log.Fatal(cerr)
			}
			log.Printf("loaded caches from %s", *fileflag)
		} else {
			log.Printf("unable to read cache file %s : %s", *fileflag, ferr.Error())
		}
	}

	// check for cache version mismatch
	if storage.Version != CANID_STORAGE_VERSION {
		log.Fatal("storage version mismatch for cache file %s: delete and try again", *fileflag)
	}

	go func() {
		http.HandleFunc("/prefix.json", storage.Prefixes.lookupServer)
		http.HandleFunc("/address.json", storage.Addresses.lookupServer)
		log.Fatal(http.ListenAndServe(":8081", nil))
	}()

	_ = <-interrupt
	log.Printf("terminating on interrupt")

	// dump cache if filename given
	if len(*fileflag) > 0 {
		outfile, ferr := os.Create(*fileflag)
		if ferr == nil {
			cerr := storage.dump(outfile)
			outfile.Close()
			if cerr != nil {
				log.Fatal(cerr)
			}
			log.Printf("dumped cache to %s", *fileflag)
		} else {
			log.Fatalf("unable to write backing file %s : %s", *fileflag, ferr.Error())
		}
	}
}
