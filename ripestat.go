package canid

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
)

// Structure partially covering the output of RIPEstat's prefix overview and
// geolocation API calls, for decoding JSON responses from RIPEstat.

type RipeStatResponse struct {
	Status string
	Data   struct {
		Resource         string
		Is_Less_Specific bool
		ASNs             []struct {
			ASN int
		}
		Locations []struct {
			Country string
		}
		Block struct {
			Resource string
		}
	}
}

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
		if doc.Data.Is_Less_Specific {
			out.Prefix = doc.Data.Resource
		} else {
			// if the resource isn't a prefix, look for the block
			out.Prefix = doc.Data.Block.Resource
		}
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

func LookupRipestat(addr net.IP) (out PrefixInfo, err error) {
	err = callRipestat(ripeStatPrefixURL, addr, &out)
	if err == nil {
		callRipestat(ripeStatGeolocURL, addr, &out)
	}
	return
}
