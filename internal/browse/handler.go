package browse

import (
	"embed"
	"log/slog"
	"net/http"
	"net/netip"

	"github.com/zmerlynn/seestar-copy/internal/state"
)

//go:embed web/*
var assets embed.FS

// Config is what the handler serves.
type Config struct {
	Addr     netip.AddrPort                          // the listener's address, with its real port
	Endpoint string                                  // shown in the page header
	Sessions *Sessions                               // the shared SMB session
	Records  func() (map[string]state.Record, error) // a destination's state; nil for no statuses
	Log      *slog.Logger
}

const csp = "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// static are the page's files, by route.
var static = map[string]struct{ file, typ string }{
	"/":          {"web/index.html", "text/html; charset=utf-8"},
	"/app.js":    {"web/app.js", "text/javascript; charset=utf-8"},
	"/style.css": {"web/style.css", "text/css; charset=utf-8"},
}

type handler struct{ cfg Config }

// NewHandler returns the browse page and its API. It answers GET and HEAD
// only, refuses a Host that is not an IP literal for cfg.Addr and any
// cross-site request, and touches neither the state nor the device until a
// request has passed every check.
func NewHandler(cfg Config) http.Handler { return &handler{cfg: cfg} }

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hd := w.Header()
	hd.Set("Content-Security-Policy", csp)
	hd.Set("Cross-Origin-Resource-Policy", "same-origin")
	hd.Set("X-Frame-Options", "DENY")
	hd.Set("Referrer-Policy", "no-referrer")
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Cache-Control", "no-store")
	reject := func(code int) { http.Error(w, "Request rejected", code) }
	if !h.hostAllowed(r.Host) {
		reject(http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		hd.Set("Allow", "GET, HEAD")
		reject(http.StatusMethodNotAllowed)
		return
	}
	asset, ok := static[r.URL.Path]
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		reject(http.StatusForbidden)
		return
	}
	b, err := assets.ReadFile(asset.file)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	hd.Set("Content-Type", asset.typ)
	w.Write(b)
}

// hostAllowed is the Host check. A Host without a port means port 80. For a
// specific address the Host must be exactly that address; for a wildcard,
// any IP literal with the listener's port. A host name is always refused.
func (h *handler) hostAllowed(host string) bool {
	if _, err := netip.ParseAddrPort(host); err != nil {
		host += ":80"
	}
	if !h.cfg.Addr.Addr().IsUnspecified() {
		return host == h.cfg.Addr.String()
	}
	ap, err := netip.ParseAddrPort(host)
	return err == nil && ap.Port() == h.cfg.Addr.Port()
}
