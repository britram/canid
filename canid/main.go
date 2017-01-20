package main

import (
	"encoding/json"
	"flag"
	"github.com/britram/canid"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
)

// RIPEstat backend moved to ripestat.go

// PrefixCache moved to prefixcache.go

// AddressCache moved to addresscache.go

const CANID_STORAGE_VERSION = 1

type CanidStorage struct {
	Version   int
	Prefixes  *canid.PrefixCache
	Addresses *canid.AddressCache
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
	storage.Prefixes = canid.NewPrefixCache(expiry, limit)
	storage.Addresses = canid.NewAddressCache(expiry, limit, storage.Prefixes)
	return storage
}

func main() {
	fileflag := flag.String("file", "", "backing store for caches (JSON file)")
	expiryflag := flag.Int("expiry", 86400, "expire cache entries after n sec")
	limitflag := flag.Int("concurrency", 16, "simultaneous backend request limit")
	portflag := flag.Int("port", 8081, "port to listen on")

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
		http.HandleFunc("/prefix.json", storage.Prefixes.LookupServer)
		http.HandleFunc("/address.json", storage.Addresses.LookupServer)
		log.Fatal(http.ListenAndServe(":"+strconv.Itoa(*portflag), nil))
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
