package synologyauth

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/ychiu1211/dsmctl/internal/gateway/platformauth"
)

var ErrUnauthorized = errors.New("DSM administrator authentication failed")

type Validator interface {
	Validate(*http.Request) (string, error)
}

type Options struct {
	Backend         *url.URL
	Signer          *platformauth.Signer
	Validator       Validator
	Logger          *slog.Logger
	RequireLoopback bool
}

func New(options Options) (http.Handler, error) {
	if options.Backend == nil || options.Backend.Scheme != "http" || options.Backend.Host == "" || options.Backend.User != nil {
		return nil, errors.New("Synology auth backend must be an absolute private HTTP URL")
	}
	if options.Signer == nil || options.Validator == nil {
		return nil, errors.New("Synology auth signer and DSM validator are required")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	proxy := httputil.NewSingleHostReverseProxy(options.Backend)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = options.Backend.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, req *http.Request, err error) {
		options.Logger.Error("gateway backend unavailable", "path", req.URL.Path, "error", err)
		http.Error(w, "gateway backend unavailable", http.StatusBadGateway)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if options.RequireLoopback && !requestFromLoopback(req) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		forwardedHost := strings.TrimSpace(req.Header.Get("X-Forwarded-Host"))
		if forwardedHost == "" {
			forwardedHost = req.Host
		}
		forwardedProto := strings.TrimSpace(req.Header.Get("X-Forwarded-Proto"))
		if forwardedProto != "http" && forwardedProto != "https" {
			forwardedProto = "http"
			if req.TLS != nil {
				forwardedProto = "https"
			}
		}
		prefix := strings.TrimRight(strings.TrimSpace(req.Header.Get("X-Forwarded-Prefix")), "/")
		if prefix != "" && strings.HasPrefix(req.URL.Path, prefix+"/") {
			req.URL.Path = strings.TrimPrefix(req.URL.Path, prefix)
		}
		req.Header.Del(platformauth.HeaderName)
		if req.URL.Path == "/admin" || strings.HasPrefix(req.URL.Path, "/admin/") {
			if !samePortalOrigin(req, forwardedProto, forwardedHost) {
				http.Error(w, "forbidden origin", http.StatusForbidden)
				return
			}
			subject, err := options.Validator.Validate(req)
			if err != nil {
				options.Logger.Warn("DSM administrator request denied", "path", req.URL.Path)
				http.Error(w, "DSM administrator authentication required", http.StatusUnauthorized)
				return
			}
			assertion, err := options.Signer.Sign(subject)
			if err != nil {
				options.Logger.Error("sign DSM administrator assertion", "error", err)
				http.Error(w, "administrator authentication unavailable", http.StatusServiceUnavailable)
				return
			}
			req.Header.Set(platformauth.HeaderName, assertion)
			req.Header.Set("X-Forwarded-Proto", forwardedProto)
			req.Header.Set("X-Forwarded-Host", forwardedHost)
			if prefix != "" {
				req.Header.Set("X-Forwarded-Prefix", prefix)
			}
			req.Header.Del("Origin")
		}
		proxy.ServeHTTP(w, req)
	}), nil
}

func samePortalOrigin(req *http.Request, scheme, host string) bool {
	origin := strings.TrimSpace(req.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" && parsed.Scheme == scheme && parsed.Host == host
}

func requestFromLoopback(req *http.Request) bool {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	address := net.ParseIP(strings.TrimSpace(host))
	return address != nil && address.IsLoopback()
}
