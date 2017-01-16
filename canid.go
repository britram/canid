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

type PrefixCache map[string]PrefixInfo

const CACHE_TIMEOUT = 60

func (cache *PrefixCache) lookup(addr net.IP) (out PrefixInfo, err error) {
	// FIXME concurrency on the underlying map, determine how ListenAndServe works...

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

		out, ok := (*cache)[prefix]
		if ok {
			// check for expiry
			if time.Since(out.Cached).Minutes() > CACHE_TIMEOUT {
				log.Printf("entry expired for prefix %s", prefix)
				delete((*cache), prefix)
			} else {
				log.Printf("cache hit! for prefix %s", prefix)
				return out, nil
			}
		}
	}

	// Cache miss, go ask RIPE
	out, err = lookupRipestat(addr)
	if err != nil {
		return
	}

	// cache and return
	out.Cached = time.Now().UTC()
	(*cache)[out.Prefix] = out
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

func (cache *PrefixCache) undump(in io.Reader) error {
	dec := json.NewDecoder(in)
	return dec.Decode(cache)
}

func (cache *PrefixCache) dump(out io.Writer) error {
	enc := json.NewEncoder(out)
	return enc.Encode(*cache)
}

// Map of names to addresses

type AddressInfo struct {
	Name      string
	Addresses []net.IP
	Cached    time.Time
}

type AddressCache map[string]AddressInfo

func (cache *AddressCache) lookup(name string) (out AddressInfo, err error) {
	// Cache lookup
	var ok bool
	out, ok = cache[name]
	if ok {
		// check for expiry
		if time.Since(out.Cached).Minutes() > CACHE_TIMEOUT {
			log.Printf("entry expired for prefix %s", prefix)
			delete((*cache), prefix)
		} else {
			log.Printf("cache hit! for prefix %s", prefix)
			return
		}
	}

	// Cache miss. Lookup.
	var addrs []net.IP
	var ainfo AddressInfo
	addrs, err = net.LookupIP(name)
	if err == nil {
		ainfo.Addresses = addrs
		ainfo.Name = name
		ainfo.Cached = time.Now().UTC()
	} else {

	}

}

func main() {
	fileflag := flag.String("file", "", "backing store for cache (JSON file)")

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	pcache := make(PrefixCache)

	flag.Parse()

	// undump cache if filename given
	if len(*fileflag) > 0 {
		infile, ferr := os.Open(*fileflag)
		if ferr == nil {
			cerr := pcache.undump(infile)
			infile.Close()
			if cerr != nil {
				log.Fatal(cerr)
			}
			log.Printf("loaded prefix cache from %s", *fileflag)
		} else {
			log.Printf("unable to read backing file %s : %s", *fileflag, ferr.Error())
		}
	}

	go func() {
		http.HandleFunc("/prefix.json", pcache.lookupServer)
		log.Fatal(http.ListenAndServe(":8081", nil))
	}()

	_ = <-interrupt
	log.Printf("terminating on interrupt")

	// dump cache if filename given
	if len(*fileflag) > 0 {
		outfile, ferr := os.Create(*fileflag)
		if ferr == nil {
			cerr := pcache.dump(outfile)
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
