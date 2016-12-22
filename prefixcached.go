package main

import (
	"encoding/json"
	"io"
	"net"
)

type PrefixInfo struct {
	Prefix      string
	ASN         int
	CountryCode string
}

type PrefixMap map[string]PrefixInfo

// RIPEstat backend

const ripeStatPrefixURL = "https://stat.ripe.net/data/prefix-overview/data.json"
const ripeStatGeolocURL = "https://stat.ripe.net/data/prefix-overview/data.json"

func parseRipeStatPrefixOverview(r io.Reader) (out PrefixInfo, err error) {

}

func parseRipeStatGeoloc(r io.Reader) error {

}

func lookupRipeStat(addr net.IP) (out PrefixInfo, err error) {

}

// API frontend
