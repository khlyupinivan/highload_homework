package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	dashboardUID  = "hw1-api-dashboard"
	dashboardName = "HW1 — CRUD API"
)

type dsRef struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

type target struct {
	RefID        string `json:"refId"`
	Expr         string `json:"expr"`
	LegendFormat string `json:"legendFormat,omitempty"`
	Datasource   dsRef  `json:"datasource"`
}

func t(refID, expr, legend string) target {
	return target{RefID: refID, Expr: expr, LegendFormat: legend}
}

type gridPos struct {
	H int `json:"h"`
	W int `json:"w"`
	X int `json:"x"`
	Y int `json:"y"`
}

type panel struct {
	ID          int                    `json:"id"`
	Title       string                 `json:"title"`
	Type        string                 `json:"type"`
	Datasource  dsRef                  `json:"datasource"`
	Targets     []target               `json:"targets"`
	GridPos     gridPos                `json:"gridPos"`
	FieldConfig map[string]interface{} `json:"fieldConfig"`
	Options     map[string]interface{} `json:"options"`
}

func graphPanel(id int, title, expr, legend, unit string, pos gridPos) panel {
	fc := map[string]interface{}{
		"defaults":  map[string]interface{}{},
		"overrides": []interface{}{},
	}
	if unit != "" {
		fc["defaults"].(map[string]interface{})["unit"] = unit
	}
	return panel{
		ID: id, Title: title, Type: "timeseries",
		Targets:     []target{t("A", expr, legend)},
		GridPos:     pos,
		FieldConfig: fc,
		Options:     map[string]interface{}{},
	}
}

func multiGraphPanel(id int, title string, targets []target, unit string, pos gridPos) panel {
	fc := map[string]interface{}{
		"defaults":  map[string]interface{}{},
		"overrides": []interface{}{},
	}
	if unit != "" {
		fc["defaults"].(map[string]interface{})["unit"] = unit
	}
	return panel{
		ID: id, Title: title, Type: "timeseries",
		Targets:     targets,
		GridPos:     pos,
		FieldConfig: fc,
		Options:     map[string]interface{}{},
	}
}

// routeInfo — описание одной ручки, как его отдаёт приложение
type routeInfo struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	ID     string `json:"id"`
}

type appMeta struct {
	Deployment string      `json:"deployment"`
	Pod        string      `json:"pod"`
	DC         string      `json:"dc"`
	Routes     []routeInfo `json:"routes"`
}

// fetchMeta забирает /__meta/routes
func fetchMeta(appURL string) (*appMeta, error) {
	var lastErr error
	for attempt := 1; attempt <= 15; attempt++ {
		resp, err := http.Get(appURL + "/__meta/routes")
		if err == nil {
			var meta appMeta
			decErr := json.NewDecoder(resp.Body).Decode(&meta)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && decErr == nil {
				return &meta, nil
			}
			lastErr = fmt.Errorf("status=%d decode=%v", resp.StatusCode, decErr)
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("no metadata from %s: %w", appURL, lastErr)
}

func panelID(base, idx int) int { return base + idx }

func buildDashboard(dsUID string, meta *appMeta) map[string]interface{} {
	ds := dsRef{Type: "prometheus", UID: dsUID}

	sel := `dc=~"$dc", pod=~"$pod", deployment=~"$deployment", route=~"$route"`

	filters := []interface{}{}
	addVar := func(name, label, query string, multi bool) {
		if query == "" {
			query = fmt.Sprintf("label_values(http_requests_total, %s)", name)
		}
		filters = append(filters, map[string]interface{}{
			"name":       name,
			"label":      label,
			"type":       "query",
			"datasource": ds,
			"query":      query,
			"includeAll": true,
			"multi":      multi,
			"refresh":    1,
			"sort":       1,
			"current":    map[string]interface{}{"text": "All", "value": "$__all"},
		})
	}
	addVar("deployment", "Deployment", "", false)
	addVar("dc", "Datacenter", "", true)
	addVar("pod", "Pod", `label_values(http_requests_total{dc=~"$dc"}, pod)`, true)
	addVar("route", "Route", "", true)

	panels := []panel{
		graphPanel(1, "RPS (requests / sec)",
			fmt.Sprintf(`sum(rate(http_requests_total{%s}[1m]))`, sel),
			"RPS", "reqps",
			gridPos{H: 8, W: 8, X: 0, Y: 0}),

		graphPanel(2, "Errors rate by class (err/s)",
			fmt.Sprintf(`sum(rate(http_errors_total{%s}[1m])) by (status_code)`, sel),
			`{{status_code}}`, "reqps",
			gridPos{H: 8, W: 8, X: 8, Y: 0}),

		multiGraphPanel(3, "Latency p50 / p75 / p90 / p95 / p99",
			[]target{
				t("A", histQuantile(0.50, sel), "p50"),
				t("B", histQuantile(0.75, sel), "p75"),
				t("C", histQuantile(0.90, sel), "p90"),
				t("D", histQuantile(0.95, sel), "p95"),
				t("E", histQuantile(0.99, sel), "p99"),
			}, "ms", gridPos{H: 8, W: 8, X: 16, Y: 0}),

		graphPanel(4, "Traffic by route (req/s)",
			fmt.Sprintf(`sum(rate(http_requests_total{%s}[1m])) by (route)`, sel),
			`{{route}}`, "reqps",
			gridPos{H: 8, W: 12, X: 0, Y: 8}),

		graphPanel(5, "Traffic by pod (req/s)",
			fmt.Sprintf(`sum(rate(http_requests_total{%s}[1m])) by (pod)`, sel),
			`{{pod}}`, "reqps",
			gridPos{H: 8, W: 12, X: 12, Y: 8}),

		graphPanel(6, "Errors by route (err/s)",
			fmt.Sprintf(`sum(rate(http_errors_total{%s}[1m])) by (route)`, sel),
			`{{route}}`, "reqps",
			gridPos{H: 8, W: 12, X: 0, Y: 16}),

		graphPanel(7, "5xx ratio (%)",
			fmt.Sprintf(`100 * sum(rate(http_errors_total{%s, status_code=~"5.."}[1m])) `+
				`/ clamp_min(sum(rate(http_requests_total{%s}[1m])), 1e-9)`+` or vector(0)`, sel, sel),
			"5xx %", "percent",
			gridPos{H: 8, W: 6, X: 12, Y: 16}),

		graphPanel(8, "p99 latency by route (ms)",
			histQuantileByRoute(0.99, sel), `{{route}}`, "ms",
			gridPos{H: 8, W: 6, X: 18, Y: 16}),
	}

	routes := make([]routeInfo, len(meta.Routes))
	copy(routes, meta.Routes)
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})

	y := 24
	for i, r := range routes {
		selRoute := fmt.Sprintf(`%s, route="%s"`, sel, r.ID)

		panels = append(panels,
			graphPanel(panelID(100, i), fmt.Sprintf("%s %s — RPS", r.Method, r.Path),
				fmt.Sprintf(`sum(rate(http_requests_total{%s}[1m])) or vector(0)`, selRoute),
				"req/s", "reqps",
				gridPos{H: 6, W: 12, X: 0, Y: y}),

			graphPanel(panelID(200, i), fmt.Sprintf("%s %s — p99 (ms)", r.Method, r.Path),
				histQuantile(0.99, selRoute), "p99, ms", "ms",
				gridPos{H: 6, W: 12, X: 12, Y: y}),
		)
		y += 6
	}

	for i := range panels {
		panels[i].Datasource = ds
		for j := range panels[i].Targets {
			panels[i].Targets[j].Datasource = ds
		}
	}

	return map[string]interface{}{
		"uid":           dashboardUID,
		"title":         dashboardName,
		"tags":          []string{"hw1", "crud-api", "ddia"},
		"timezone":      "browser",
		"schemaVersion": 39,
		"refresh":       "10s",
		"editable":      false,
		"time":          map[string]string{"from": "now-15m", "to": "now"},
		"templating":    map[string]interface{}{"list": filters},
		"panels":        panels,
	}
}

func histQuantile(q float64, sel string) string {
	return fmt.Sprintf(
		`histogram_quantile(%.2f, sum(rate(http_request_latency_seconds_bucket{%s}[5m])) by (le)) * 1000`,
		q, sel)
}

func histQuantileByRoute(q float64, sel string) string {
	return fmt.Sprintf(
		`histogram_quantile(%.2f, sum(rate(http_request_latency_seconds_bucket{%s}[5m])) by (le, route)) * 1000`,
		q, sel)
}

func doJSON(c *http.Client, method, url, token string, payload interface{}) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, nil
}

func waitForGrafana(c *http.Client, baseURL string) error {
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := c.Get(baseURL + "/api/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("grafana is not ready")
}

func readToken() (string, error) {
	if v := os.Getenv("GRAFANA_TOKEN"); v != "" {
		return v, nil
	}
	path := os.Getenv("GF_TOKEN_FILE")
	if path == "" {
		path = "/secrets/grafana-token"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read token file %s: %w", path, err)
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Errorf("token file %s is empty", path)
	}
	return tok, nil
}

func main() {
	grafanaURL := os.Getenv("GF_URL")
	if grafanaURL == "" {
		grafanaURL = "http://grafana:3000"
	}
	dsUID := os.Getenv("DS_UID")
	if dsUID == "" {
		dsUID = "vm"
	}
	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://api:8080"
	}

	token, err := readToken()
	if err != nil {
		log.Fatalf("token: %v", err)
	}
	log.Printf("using grafana=%s ds_uid=%s token=true", grafanaURL, dsUID)

	meta, err := fetchMeta(appURL)

	if err != nil {
		log.Fatalf("fetch routes: %v", err)
	}
	log.Printf("discovered %d routes from %s (deployment=%s dc=%s pod=%s)",
		len(meta.Routes), appURL, meta.Deployment, meta.DC, meta.Pod)

	c := &http.Client{Timeout: 10 * time.Second}

	if err := waitForGrafana(c, grafanaURL); err != nil {
		log.Fatalf("wait: %v", err)
	}

	payload := map[string]interface{}{
		"dashboard": buildDashboard(dsUID, meta),
		"overwrite": true,
		"message":   "auto-generated by dashboard-gen",
	}

	status, raw, err := doJSON(c, http.MethodPost,
		grafanaURL+"/api/dashboards/db", token, payload)
	if err != nil {
		log.Fatalf("push dashboard: %v", err)
	}
	if status != 200 {
		log.Fatalf("grafana returned %d: %s", status, string(raw))
	}

	var resp struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(raw, &resp)
	log.Printf("dashboard %q created: %s%s", dashboardName, grafanaURL, resp.URL)
}
