package main

import (
	"encoding/json"
	"errors"
	"flag"
	//"fmt"
	//"io"
	"log"
	"net"
	"net/http"
	"net/url"
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
}

// RIPEstat backend

const ripeStatPrefixURL = "https://stat.ripe.net/data/prefix-overview/data.json"
const ripeStatGeolocURL = "https://stat.ripe.net/data/geoloc/data.json"

func call_ripestat(apiurl string, addr net.IP, out *PrefixInfo) error {

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

func lookup_ripestat(addr net.IP) (out PrefixInfo, err error) {
	err = call_ripestat(ripeStatPrefixURL, addr, &out)
	if err == nil {
		call_ripestat(ripeStatGeolocURL, addr, &out)
	}
	return
}

// Map of prefixes to information about them, stored by prefix.

type PrefixCache map[string]PrefixInfo

func (cache *PrefixCache) lookup(addr net.IP) (out PrefixInfo, err error) {

	// Determine starting prefix
	var prefixlen, addrbits int
	if len(addr) == 4 {
		prefixlen = 24
		addrbits = 32
	} else {
		prefixlen = 48
		addrbits = 128
	}

	// Iterate through prefixes looking for a match

	for i := prefixlen; i > 0; i-- {
		mask := net.CIDRMask(prefixlen, addrbits)
		net := net.IPNet{addr.Mask(mask), mask}
		prefix := net.String()

		out, ok := (*cache)[prefix]
		if ok {
			log.Printf("cache hit! for prefix %s", prefix)
			return out, nil
		}
		log.Printf("cache miss for prefix %s", prefix)
	}

	// Cache miss, go ask RIPE
	out, err = lookup_ripestat(addr)
	if err != nil {
		return
	}

	// cache and return
	(*cache)[out.Prefix] = out
	return
}

func (cache *PrefixCache) lookup_server(w http.ResponseWriter, req *http.Request) {

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

func main() {
	flag.Parse()

	cache := make(PrefixCache)

	http.HandleFunc("/prefix.json", cache.lookup_server)
	log.Fatal(http.ListenAndServe(":8081", nil))
}

// API frontend
