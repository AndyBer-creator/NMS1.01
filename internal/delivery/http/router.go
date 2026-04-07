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
	// Auth (Basic): viewer/admin. If creds are not set -> open access.
	r.Use(RequireAuth)

	// Static assets served from the container image.
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))

	// Dashboard
	r.Get("/", handlers.Dashboard)
	r.Get("/health", handlers.Health)

	// Devices
	r.Get("/devices", handlers.ListDevices)
	r.Get("/devices/list", handlers.DevicesListPage)
	r.Get("/devices/table", handlers.DevicesTable)
	r.Get("/devices/page", handlers.DevicesPage)
	r.Get("/devices/{ip}/metric/{oid}", handlers.GetMetric)
	r.With(RequireAdmin).Post("/devices", handlers.CreateDevice)
	r.With(RequireAdmin).Delete("/devices/{ip}", handlers.DeleteDevice)
	r.With(RequireAdmin).Post("/devices/{ip}/snmp/set", handlers.SetSNMP)

	r.With(RequireAdmin).Post("/discovery/scan", handlers.DiscoverScan)

	// MIB: панель доступна всем с ролью (viewer видит сообщение), загрузка/удаление — только admin
	r.Get("/mibs/panel", handlers.MibPanel)
	r.Post("/mibs/resolve", handlers.MibResolve)
	r.With(RequireAdmin).Post("/mibs/upload", handlers.MibUpload)
	r.With(RequireAdmin).Post("/mibs/delete", handlers.MibDelete)

	// LLDP topology
	r.Get("/topology/lldp", handlers.LldpTopologyPage)
	r.Get("/topology/lldp/data", handlers.LldpTopologyData)

	// Prometheus metrics endpoint
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/traps", func(r chi.Router) {
		r.Get("/page", handlers.TrapsPage)
		r.Get("/", handlers.ListTraps)
		r.Get("/{ip}", handlers.TrapsByDevice)
	})

	r.With(RequireAdmin).Post("/test-alert", handlers.testAlert)

	return r
}
