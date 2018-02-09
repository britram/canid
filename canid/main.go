package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"

	"github.com/britram/canid"
)

// WelcomePage contains the Canid welcome page, which explains what Canid is,
// and gives a simple web interface to the service.
const WelcomePage = `
<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf8">
    <title>Canid, the Caching Additional Network Information Daemon</title>

    <link href='https://fonts.googleapis.com/css?family=Lato' rel='stylesheet'>

    <style>

      body {
        background: #cccccc;
        font-family: "Lato";
      }

      div.content {        
        background: #eeeeee;
        width: 600px;
        padding: 40px;
        margin: auto;
        border: 3px solid grey;
      }

      div.output {
        width: 80%;
        border: 1px solid grey;
        margin-left: auto;
        margin-right: auto;
        margin-bottom: 4px;
        padding: 2px;  
        background: white;    
      }

      div#result {
        height: 400px;
      }

      div#status {
        height: 40px;
      }

      input#content-url {
        width: 75%;
      }

      span.paper-title {
        font-style: italic;
      }
      
      h1 {
        border-bottom: 2px solid #333333;
      }

      h2 {
        border-bottom: 1px solid #666666;
      }

      img#mami-logo {
        display: block;
        margin-left: auto;
        margin-right: auto;
        width: 50%;
      }

      label, input {
        display: inline-block;
      }

      label {
        width: 25%;
        text-align: right;
      }

      label + input {
        width: 40%;
        margin: 0 15% 0 4%;
      }

      input + input {
        float: right;
      }
    </style>
    
    <script>

      async function canidLookupPrefix() {

        const inputElement = document.getElementById('input')
        const statusElement = document.getElementById('status')
        const prefixElement = document.getElementById('prefix')
        const asElement = document.getElementById('as')
        const ccElement = document.getElementById('cc')

        try {
          let response = await fetch("/prefix.json?addr="+encodeURIComponent(inputElement.value))
          let result = await response.json()

          statusElement.value = "prefix lookup "+inputElement.value+" OK"
          prefixElement.value = result.Prefix 
          asElement.value = result.ASN 
          ccElement.value = result.CountryCode 
        } catch (error) {
          statusElement.value = "prefix lookup "+inputElement.value+" failed; see console"
          console.log(error)
        }
      }

    </script>
  </head>
  <body>

    <div class="content">

      <h1>Canid<sup>beta</sup></h1>

      <p>Canid, the Caching Additional Network Information Daemon, provides a
      simple HTTP API for getting information about Internet names and
      numbers. See <a href="https://github.com/britram/canid">the GitHub
      repository</a> for source code and more information</p>

      <p>This landing page provides a browser-based interface to this instance
      of the cache, backed by <a href="https://stat.ripe.net">RIPEstat</a>. You
      can perform prefix lookups using the form below.</p>

      <div class="tool"><form>

        <div>
          <label>Address to query:</label> <input type="text" id="input">
        </div>
       <hr>
        <div>
            <label>Status:</label> <input type="text" disabled id="status" value="Ready">
        </div>

        <div>
            <label>Prefix:</label> <input type="text" disabled id="prefix">
        </div>
  
        <div>
            <label>BGP ASN:</label> <input type="text" disabled id="prefix">
        </div>

        <div>
            <label>Country:</label> <input type="text" disabled id="prefix">
        </div>

        <input type="button" id="pfxGoButton" onclick="canidLookupPrefix()" value="Look up prefix">

      </form></div>
    </div>
  </body>
</html>
`

const canidStorageVersion = 1

type canidStorage struct {
	Version   int
	Prefixes  *canid.PrefixCache
	Addresses *canid.AddressCache
}

func (storage *canidStorage) undump(in io.Reader) error {
	dec := json.NewDecoder(in)
	return dec.Decode(storage)
}

func (storage *canidStorage) dump(out io.Writer) error {
	enc := json.NewEncoder(out)
	return enc.Encode(*storage)
}

func newStorage(expiry int, limit int) *canidStorage {
	storage := new(canidStorage)
	storage.Version = canidStorageVersion
	storage.Prefixes = canid.NewPrefixCache(expiry, limit)
	storage.Addresses = canid.NewAddressCache(expiry, limit, storage.Prefixes)
	return storage
}

func welcomeServer(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(WelcomePage))
}

func main() {
	fileflag := flag.String("file", "", "backing store for caches (JSON file)")
	expiryflag := flag.Int("expiry", 86400, "expire cache entries after n sec")
	limitflag := flag.Int("concurrency", 16, "simultaneous backend request limit")
	portflag := flag.Int("port", 8043, "port to listen on")

	// parse command line
	flag.Parse()

	// set up sigint handling
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
	if storage.Version != canidStorageVersion {
		log.Fatalf("storage version mismatch for cache file %s: delete and try again", *fileflag)
	}

	go func() {
		http.HandleFunc("/", welcomeServer)
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
