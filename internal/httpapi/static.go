package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func NewSPAHandler(directory string) (http.Handler, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil, errors.New("static directory is required")
	}
	indexPath := filepath.Join(directory, "index.html")
	if info, err := os.Stat(indexPath); err != nil || info.IsDir() {
		if err == nil {
			err = errors.New("index.html is a directory")
		}
		return nil, err
	}
	fileServer := http.FileServer(http.Dir(directory))
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		cleanPath := path.Clean("/" + request.URL.Path)
		relativePath := strings.TrimPrefix(cleanPath, "/")
		candidate := filepath.Join(directory, filepath.FromSlash(relativePath))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if strings.Contains(filepath.Base(candidate), ".") && relativePath != "index.html" {
				response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				response.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(response, request)
			return
		}
		if filepath.Ext(relativePath) != "" {
			http.NotFound(response, request)
			return
		}
		requestCopy := request.Clone(request.Context())
		requestCopy.URL.Path = "/"
		response.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(response, requestCopy)
	})
	return securityHeaders(handler), nil
}
