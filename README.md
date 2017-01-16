# canid

the Caching Additional Network Information Daemon provides a simple HTTP API for getting information about Internet names and numbers from a given vantage point.

canid has two entry points, one of which works:

- `/prefix.json?addr=<IPv4 or IPv6 address>` looks up BGP AS number and country code associated with the smallest prefix announced which contains the address in the RIPEstat database. It caches the results by prefix in memory. It returns a JSON object with four keys:
    - Prefix: CIDR-notation prefix associated with the address
    - ASN: First AS number associated with the prefix by RIPEstat
    - CountryCode: First country code associated with the prefix by RIPEstat
    - Cached: Timestamp at which the result was cached from RIPEstat

- `/addresses.json?name=<Internet name>` looks up the IPv4 and IPv6 addresses associated with a given name. It caches the results by name in memory, and precaches prefix results for a subsequent prefix call. It returns a JSON array of addresses as strings.
