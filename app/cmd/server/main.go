package main

import (
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"example.com/hw1/api/internal/gsmetrics"
	"example.com/hw1/api/internal/httpx"
	"example.com/hw1/api/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		c := http.Client{Timeout: 2 * time.Second}
		resp, err := c.Get("http://127.0.0.1:8080/healthz")
		if err != nil {
			os.Exit(1)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	reg := prometheus.NewRegistry()
	m := gsmetrics.New(reg)
	id := gsmetrics.IdentityFromEnv()

	src := httpx.NewRouter()
	db := store.New()
	store.Register(src, db)

	top := http.NewServeMux()
	top.Handle("/metrics", m.WrapMetrics(promhttp.HandlerFor(reg, promhttp.HandlerOpts{})))
	top.HandleFunc("/__meta/routes", gsmetrics.RoutesHandler(id.Deployment, id.Pod, id.DC, func() []gsmetrics.RouteInfo {
		out := make([]gsmetrics.RouteInfo, 0, len(src.Routes()))
		for _, r := range src.Routes() {
			out = append(out, gsmetrics.RouteInfo{
				Method: r.Method, Path: r.Path, ID: httpx.RouteID(r.Method, r.Path),
			})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return out
	}))

	for _, r := range src.Routes() {
		rt := r
		top.Handle(rt.Method+" "+rt.Path,
			gsmetrics.Chain(rt.Handler, m.Middleware(httpx.RouteID(rt.Method, rt.Path))))
	}

	srv := &http.Server{Addr: ":8080", Handler: top, ReadHeaderTimeout: 2 * time.Second}
	log.Printf("started %s pod=%s dc=%s", id.Deployment, id.Pod, id.DC)
	log.Fatal(srv.ListenAndServe())
}
