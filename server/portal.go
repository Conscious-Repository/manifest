package server

import (
	"io/fs"
	"net/http"
)

// PortalHandler serves the AION portal as a standalone static site, rooted at
// the embedded web/portal subtree (the copy of the aionbio public/portal app).
//
// This is a SEPARATE mux from Handler(): it is mounted on its own listener
// (cfg.PortalPort, default 7778) and shares nothing with the dashboard's
// routes. GET / returns web/portal/index.html; every other path resolves
// against the portal's own assets (src/*, data/*, content/*), which the app
// requests with document-relative URLs — so the portal is self-contained at
// the root of its port.
//
// The AION portal move, phase 1: additive only. Nothing here is wired into
// the dashboard mux, the 7777 listener, or the aionbio publish path.
func PortalHandler() (http.Handler, error) {
	sub, err := fs.Sub(webFiles, "web/portal")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/", noCache(http.FileServer(http.FS(sub))))
	return mux, nil
}
