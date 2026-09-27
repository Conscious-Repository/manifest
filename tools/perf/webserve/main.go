// webserve serves the embedded front end through the app's own web handler
// (server.WebHandler: the shell, every asset, the production cache headers)
// and forwards /api/ and the stub's /__ hooks to a chat stub, so a browser
// measurement (tools/perf/chat-perf.cjs) sees the requests and caching the
// owner's browser sees, over fixture data instead of the owner's.
//
//	webserve -addr 127.0.0.1:0 -api http://127.0.0.1:PORT
//
// It prints "listening http://HOST:PORT" once ready.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"manifest/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "listen address")
	api := flag.String("api", "", "chat stub origin that answers /api/ and /__ hooks")
	flag.Parse()
	target, err := url.Parse(*api)
	if err != nil || target.Host == "" {
		log.Fatal("webserve: -api must be the stub's origin")
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1 // event streams pass through as they are written
	web := server.WebHandler()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/__") {
			proxy.ServeHTTP(w, r)
			return
		}
		web.ServeHTTP(w, r)
	})
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("listening http://" + ln.Addr().String())
	log.Fatal(http.Serve(ln, h))
}
