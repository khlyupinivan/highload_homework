package httpx

import (
	"net/http"
	"strings"
	"sync"
)

type Route struct {
	Method      string
	Path        string
	Description string
	Handler     http.HandlerFunc
}

// Router — роутер со списком маршрутов в рантайме
type Router struct {
	mu     sync.RWMutex
	routes []Route
	mux    *http.ServeMux
}

func NewRouter() *Router { return &Router{mux: http.NewServeMux()} }

func (r *Router) Handle(method, path, desc string, h http.HandlerFunc) {
	r.mu.Lock()
	r.routes = append(r.routes, Route{
		Method:      method,
		Path:        path,
		Description: desc,
		Handler:     h,
	})
	r.mu.Unlock()

	pattern := method + " " + path
	r.mux.HandleFunc(pattern, h)
}

// Возвращает копию списка ручек
func (r *Router) Routes() []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Route, len(r.routes))
	copy(out, r.routes)
	return out
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// "POST /users" -> "users.create"
func RouteID(method, path string) string {
	seg := strings.Trim(path, "/")
	seg = strings.ReplaceAll(seg, "{", "")
	seg = strings.ReplaceAll(seg, "}", "")
	name := strings.ReplaceAll(seg, "/", "_")
	if name == "" {
		name = "root"
	}
	return name + "." + strings.ToLower(method)
}
