package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

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
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		cleanPath := path.Clean("/" + request.URL.Path)
		relativePath := strings.TrimPrefix(cleanPath, "/")
		candidate := filepath.Join(directory, filepath.FromSlash(relativePath))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(response, request)
			return
		}
		if filepath.Ext(relativePath) != "" {
			http.NotFound(response, request)
			return
		}
		requestCopy := request.Clone(request.Context())
		requestCopy.URL.Path = "/"
		fileServer.ServeHTTP(response, requestCopy)
	}), nil
}
