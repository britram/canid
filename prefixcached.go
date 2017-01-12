package main

import (
	"encoding/json"
	"errors"
	//"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	//"strconv"
)

type PrefixInfo struct {
	Prefix      string
	ASN         int
	CountryCode string
}

type PrefixMap map[string]PrefixInfo

// RIPEstat backend

const ripeStatPrefixURL = "https://stat.ripe.net/data/prefix-overview/data.json"
const ripeStatGeolocURL = "https://stat.ripe.net/data/geoloc/data.json"

func parseRipeStatPrefixOverview(body io.ReadCloser, out *PrefixInfo) (err error) {

	doc := make(map[string]interface{})

	dec := json.NewDecoder(body)
	err = dec.Decode(&doc)
	if err != nil {
		return
	}

	log.Printf("got prefix doc %#v", doc)

	datax, ok := doc["data"]
	if !ok {
		err = errors.New("missing data in prefix-overview")
		return
	}
	data := datax.(map[string]interface{})

	pfx := data["resource"].(string)
	if len(out.Prefix) == 0 {
		out.Prefix = pfx
	} else if pfx != out.Prefix {
		err = errors.New("prefix mismatch in prefix-overview")
	}

	out.Prefix = data["resource"].(string)

	asarrayx, ok := data["asns"]
	if ok {
		asarray := asarrayx.([]interface{})
		out.ASN = int(asarray[0].(map[string]interface{})["asn"].(float64))
	}

	return
}

func parseRipeStatGeoloc(body io.ReadCloser, out *PrefixInfo) (err error) {

	doc := make(map[string]interface{})

	dec := json.NewDecoder(body)
	err = dec.Decode(&doc)
	if err != nil {
		return
	}

	log.Printf("got geoloc doc %#v", doc)

	datax, ok := doc["data"]
	if !ok {
		err = errors.New("missing data in geoloc")
		return
	}
	data := datax.(map[string]interface{})

	pfx := data["resource"].(string)
	if len(out.Prefix) == 0 {
		out.Prefix = pfx
	}

	locarrayx, ok := data["locations"]
	if ok {
		locarray := locarrayx.([]interface{})
		out.CountryCode = locarray[0].(map[string]interface{})["country"].(string)
	}

	return
}

func lookupRipeStat(addr net.IP) (out PrefixInfo, err error) {

	v := make(url.Values)

	v.Add("resource", addr.String())

	// step 1, prefix overview
	// add query string
	var prefixUrl *url.URL
	prefixUrl, err = url.Parse(ripeStatPrefixURL)
	if err != nil {
		return
	}
	prefixUrl.RawQuery = v.Encode()

	// make API call
	var resp *http.Response
	resp, err = http.Get(prefixUrl.String())
	if err != nil {
		return
	}

	// parse output
	err = parseRipeStatPrefixOverview(resp.Body, &out)
	if err != nil {
		return
	}

	// step 2, geolocation
	// add query string
	prefixUrl, err = url.Parse(ripeStatGeolocURL)
	if err != nil {
		return
	}
	prefixUrl.RawQuery = v.Encode()

	// make API call
	resp, err = http.Get(prefixUrl.String())
	if err != nil {
		return
	}

	// parse output
	err = parseRipeStatGeoloc(resp.Body, &out)
	if err != nil {
		return
	}

	return
}

func main() {
	pi, err := lookupRipeStat(net.ParseIP("5.148.172.66"))
	if err != nil {
		log.Fatal(err.Error())
	}

	fmt.Printf("%s is in %s (AS %d) (CC %2s)\n", "5.148.172.66", pi.Prefix, pi.ASN, pi.CountryCode)
}

// API frontend
