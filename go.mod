module airplay-wifi-setup

go 1.22

require (
	github.com/BertoldVdb/WACResearch v0.0.0
	github.com/grandcat/zeroconf v1.0.0
)

require (
	github.com/aead/ecdh v0.2.0 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/miekg/dns v1.1.35 // indirect
	go.mozilla.org/pkcs7 v0.0.0-20210826202110-33d05740a352 // indirect
	golang.org/x/crypto v0.0.0-20200820211705-5c72a883971a // indirect
	golang.org/x/net v0.0.0-20201110031124-69a78807bb2b // indirect
	golang.org/x/sys v0.0.0-20200930185726-fdedc70b468f // indirect
)

// WACResearch is fetched and patched by build.sh into ./build
replace github.com/BertoldVdb/WACResearch => ./build
