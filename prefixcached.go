package main

import (
	"encoding/json"
	"flag"
	"io"
	"net"
	"strconv"
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

func parseRipeStatPrefixOverview(data []byte, out PrefixInfo) (err error) {

	var doc map[string]interface{}

	err = json.Unmarshal(data, doc)
	if err != nil {
		return
	}

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

	asarrayx, ok := doc["asns"]
	if ok {
		asarray = asarrayx.([]map[string]interface{})
		out.ASN = strconv.Atoi(asarray[0]["asn"].(string))
	}
}

func parseRipeStatGeoloc(data []byte, out PrefixInfo) (err error) {
	var doc map[string]interface{}

	err = json.Unmarshal(data, doc)
	if err != nil {
		return
	}

	datax, ok := doc["data"]
	if !ok {
		err = errors.New("missing data in geoloc")
		return
	}
	data := datax.(map[string]interface{})

	pfx := data["resource"].(string)
	if len(out.Prefix) == 0 {
		out.Prefix = pfx
	} else if pfx != out.Prefix {
		err = errors.New("prefix mismatch in geoloc")
	}

	locarrayx, ok := doc["locations"]
	if ok {
		locarray = locarrayx.([]map[string]interface{})
		out.CountryCode = asarray[0]["country"].(string)
	}
}

func lookupRipeStat(addr net.IP) (out PrefixInfo, err error) {

}

// API frontend
