package gsmetrics

import "net/http"

// RoutesHandler отдаёт {"deployment": ..., "pod": ..., "dc": ..., "routes": [{method,path,id}]}.
func RoutesHandler(dep, pod, dc string, routes func() []RouteInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{
			"deployment": dep, "pod": pod, "dc": dc, "routes": routes(),
		}
		_ = jsonEncode(w, body)
	}
}

type RouteInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	ID     string `json:"id"`
}
