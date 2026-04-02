package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func Router(handlers *Handlers) *chi.Mux {
	r := chi.NewRouter()
	// Metrics + logging + panic recovery
	r.Use(PrometheusMetrics, middleware.Logger, middleware.Recoverer)

	// Static assets served from the container image.
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	// Dashboard
	r.Get("/", handlers.Dashboard)
	r.Get("/health", handlers.Health)

	// Devices
	r.Get("/devices", handlers.ListDevices)
	r.Get("/devices/table", handlers.DevicesTable)
	r.Get("/devices/page", handlers.DevicesPage)
	r.Get("/devices/{ip}/metric/{oid}", handlers.GetMetric)
	r.Post("/devices", handlers.CreateDevice)
	r.Delete("/devices/{ip}", handlers.DeleteDevice)
	r.Post("/devices/{ip}/snmp/set", handlers.SetSNMP)

	r.Post("/discovery/scan", handlers.DiscoverScan)

	// Prometheus metrics endpoint
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/traps", func(r chi.Router) {
		r.Get("/", handlers.ListTraps)
		r.Get("/{ip}", handlers.TrapsByDevice)
	})

	r.Post("/test-alert", handlers.testAlert)

	return r
}
