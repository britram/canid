# prefixcache
High-speed cached lookup of IP address to prefix/ASN/CC information

This utility provides a RESTful API for looking up information about IPv4 
and IPv6 addresses. This information is cached in memory for high-speed 
lookup, and can be backed by any number of sources of information: ndjson files
on disk, [RIPEstat](https://stat.ripe.net), etc.
