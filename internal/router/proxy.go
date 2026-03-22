package router

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

// Proxy wraps common reverse-proxy logic for forwarding requests to sandbox
// backends. Proxy instances are cached per target host for connection reuse.
type Proxy struct {
	logger  *slog.Logger
	proxies sync.Map // map[string]*httputil.ReverseProxy

	transport http.RoundTripper
}

// NewProxy returns a Proxy with default settings and a shared transport.
func NewProxy(logger *slog.Logger) *Proxy {
	return &Proxy{
		logger: logger,
		transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
}

// Forward proxies the incoming request to the given targetHost. Proxy instances
// are cached by targetHost for connection reuse.
func (p *Proxy) Forward(w http.ResponseWriter, req *http.Request, targetHost string) {
	rp := p.getOrCreateProxy(targetHost)
	rp.ServeHTTP(w, req)
}

func (p *Proxy) getOrCreateProxy(targetHost string) *httputil.ReverseProxy {
	if val, ok := p.proxies.Load(targetHost); ok {
		return val.(*httputil.ReverseProxy)
	}

	target := &url.URL{
		Scheme: "http",
		Host:   targetHost,
	}

	proxy := &httputil.ReverseProxy{
		Director: func(outReq *http.Request) {
			outReq.URL.Scheme = target.Scheme
			outReq.URL.Host = target.Host
			outReq.Host = target.Host

			if _, ok := outReq.Header["User-Agent"]; !ok {
				outReq.Header.Set("User-Agent", "")
			}
		},
		Transport: p.transport,
		ErrorHandler: func(rw http.ResponseWriter, r *http.Request, err error) {
			p.logger.Error("proxy error", "target", targetHost, "error", err)
			http.Error(rw, "bad gateway", http.StatusBadGateway)
		},
	}

	actual, _ := p.proxies.LoadOrStore(targetHost, proxy)
	return actual.(*httputil.ReverseProxy)
}
