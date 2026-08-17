package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"manifest/fundraising"
	"manifest/fundraisingportal"
	"manifest/teamportal"
)

type FundraisingPortalOptions struct {
	Auth       *teamportal.Auth
	Invites    *fundraisingportal.InviteStore
	Store      *fundraising.Store
	Snapshot   func() []fundraising.Opportunity
	AdminEmail string
	AuditPath  string
}

type fundraisingPortalAPI struct {
	opt FundraisingPortalOptions
	mu  sync.Mutex
}

// FundraisingPortalHandler is deliberately separate from PortalHandler: it
// exposes only flattened fundraising records to an explicit invite list.
func FundraisingPortalHandler(opt FundraisingPortalOptions) (http.Handler, error) {
	sub, err := fs.Sub(webFiles, "web/fundraising-portal")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/", noCache(etagFor(sub), http.FileServer(http.FS(sub))))
	if opt.Auth != nil {
		mux.HandleFunc("GET /oauth2/login", opt.Auth.HandleLogin)
		mux.HandleFunc("GET /oauth2/callback", opt.Auth.HandleCallback)
		mux.HandleFunc("POST /oauth2/logout", opt.Auth.HandleLogout)
	}
	api := &fundraisingPortalAPI{opt: opt}
	mux.HandleFunc("GET /api/me", api.me)
	mux.HandleFunc("GET /api/fundraising", api.list)
	mux.HandleFunc("POST /api/fundraising/item", api.create)
	mux.HandleFunc("PATCH /api/fundraising/{id...}", api.patch)
	login, _ := fs.ReadFile(sub, "login.html")
	return requireFundraisingInvite(opt, mux, login), nil
}

func requireFundraisingInvite(opt FundraisingPortalOptions, inner http.Handler, login []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/oauth2/") || (r.Method == http.MethodGet && (r.URL.Path == "/style.css" || strings.HasPrefix(r.URL.Path, "/assets/"))) {
			inner.ServeHTTP(w, r)
			return
		}
		if opt.Auth != nil && opt.Invites != nil {
			if id, ok := opt.Auth.Identify(r); ok && opt.Invites.Allowed(id.Email) {
				inner.ServeHTTP(w, r)
				return
			}
		}
		if isBrowserNav(r) && len(login) > 0 {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(login)
			return
		}
		http.Error(w, "invite required", http.StatusUnauthorized)
	})
}

func (a *fundraisingPortalAPI) identity(r *http.Request) teamportal.Identity {
	id, _ := a.opt.Auth.Identify(r)
	return id
}

func (a *fundraisingPortalAPI) me(w http.ResponseWriter, r *http.Request) {
	id := a.identity(r)
	writeJSON(w, map[string]any{"email": id.Email, "name": id.Name, "admin": strings.EqualFold(id.Email, a.opt.AdminEmail)})
}

func (a *fundraisingPortalAPI) list(w http.ResponseWriter, _ *http.Request) {
	ops := a.snapshot()
	out := make([]fundraising.SharedOpportunity, 0, len(ops))
	for _, op := range ops {
		out = append(out, fundraising.SharedFromOpportunity(op))
	}
	writeJSON(w, map[string]any{"opportunities": out, "statuses": fundraising.Statuses, "interests": fundraising.Interests})
}

func (a *fundraisingPortalAPI) create(w http.ResponseWriter, r *http.Request) {
	var body fundraising.SharedOpportunity
	if err := decode(r, &body); err != nil {
		httpError(w, err)
		return
	}
	op, err := a.opt.Store.CreateShared(body)
	if err != nil {
		httpError(w, err)
		return
	}
	a.audit(a.identity(r), "create", op.ID, []string{"firm"})
	writeJSON(w, a.shared(op))
}

func (a *fundraisingPortalAPI) patch(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := decode(r, &body); err != nil {
		httpError(w, err)
		return
	}
	op, err := a.opt.Store.SharedPatch(r.PathValue("id"), body)
	if err != nil {
		httpError(w, err)
		return
	}
	fields := make([]string, 0, len(body))
	for field := range body {
		fields = append(fields, field)
	}
	a.audit(a.identity(r), "edit", op.ID, fields)
	writeJSON(w, a.shared(op))
}

func (a *fundraisingPortalAPI) snapshot() []fundraising.Opportunity {
	if a.opt.Snapshot != nil {
		return a.opt.Snapshot()
	}
	ops, _ := a.opt.Store.List()
	return ops
}

func (a *fundraisingPortalAPI) shared(fallback fundraising.Opportunity) fundraising.SharedOpportunity {
	for _, op := range a.snapshot() {
		if op.ID == fallback.ID {
			return fundraising.SharedFromOpportunity(op)
		}
	}
	return fundraising.SharedFromOpportunity(fallback)
}

func (a *fundraisingPortalAPI) audit(actor teamportal.Identity, action, id string, fields []string) {
	if a.opt.AuditPath == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(a.opt.AuditPath), 0o700)
	f, err := os.OpenFile(a.opt.AuditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(map[string]any{"at": time.Now().UTC().Format(time.RFC3339), "actor": actor.Email, "name": actor.Name, "action": action, "id": id, "fields": fields})
	_, _ = f.Write(append(b, '\n'))
}
