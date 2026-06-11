// Package proxy provides a reverse proxy for forwarding /api/* requests to an upstream.
package proxy

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// ReverseProxy creates an httputil.ReverseProxy that forwards requests to the
// given upstream URL. Only non-auth, non-sync API paths are proxied.
func ReverseProxy(upstream string) http.Handler {
	target, err := url.Parse(upstream)
	if err != nil {
		log.Printf("[proxy] invalid upstream URL %q: %v", upstream, err)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(`{"error":"invalid upstream configuration"}`))
		})
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorLog = log.Default()

	proxy.Rewrite = func(pr *httputil.ProxyRequest) {
		pr.SetURL(target)
		pr.Out.Host = target.Host

		// Strip /api prefix when forwarding to upstream
		cleanPath := strings.TrimPrefix(pr.In.URL.Path, "/api")
		if !strings.HasPrefix(cleanPath, "/") {
			cleanPath = "/" + cleanPath
		}
		pr.Out.URL.Path = cleanPath
		pr.Out.URL.RawQuery = pr.In.URL.RawQuery

		log.Printf("[proxy] %s %s → %s%s", pr.In.Method, pr.In.URL.Path, upstream, cleanPath)
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		log.Printf("[proxy] %s %s → %d", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode)
		return nil
	}

	return proxy
}
