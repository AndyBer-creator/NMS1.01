package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func Router(handlers *Handlers) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(PrometheusMetrics, middleware.Logger, middleware.Recoverer)
	r.Use(SecurityHeaders)
	r.Use(EnforceHTTPS)

	// Без авторизации: статика, проверки для оркестраторов, метрики, вход/выход
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	r.Get("/health", handlers.Health)
	r.Get("/ready", handlers.Ready)
	r.Get("/.well-known/security.txt", serveWellKnownSecurityTxt)
	r.Handle("/metrics", promhttp.Handler())

	r.Get("/login", handlers.LoginPage)
	r.Post("/login", handlers.LoginPost)
	r.Post("/alerts/webhook", handlers.AlertWebhook)

	r.Group(func(r chi.Router) {
		r.Use(RequireAuth)
		r.Use(RequireCSRF)

		r.Get("/", handlers.Dashboard)
		r.Post("/logout", handlers.Logout)

		r.Get("/devices", handlers.ListDevices)
		r.Get("/devices/list", handlers.DevicesListPage)
		r.Get("/devices/table", handlers.DevicesTable)
		r.Get("/devices/page", handlers.DevicesPage)
		r.Get("/devices/{id}/metric/{oid}", handlers.GetMetric)
		r.With(RequireAdmin).Post("/devices", handlers.CreateDevice)
		r.With(RequireAdmin).Get("/devices/{id}/edit", handlers.EditDeviceRow)
		r.With(RequireAdmin).Post("/devices/{id}", handlers.UpdateDevice)
		r.With(RequireAdmin).Delete("/devices/{id}", handlers.DeleteDevice)
		r.With(RequireAdmin).Post("/devices/{id}/snmp/set", handlers.SetSNMP)
		r.With(RequireAdmin).Get("/devices/{id}/terminal", handlers.TerminalPage)
		r.With(RequireAdmin).Get("/ws/terminal/{id}", handlers.TerminalWS)

		r.With(RequireAdmin).Post("/discovery/scan", handlers.DiscoverScan)

		r.Get("/settings/worker-poll-panel", handlers.WorkerPollSettingsPanel)
		r.With(RequireAdmin).Post("/settings/worker-poll-interval", handlers.SetWorkerPollInterval)
		r.Get("/settings/alert-email-panel", handlers.AlertEmailPanel)
		r.With(RequireAdmin).Post("/settings/alert-email", handlers.SetAlertEmail)

		r.Get("/mibs/panel", handlers.MibPanel)
		r.Post("/mibs/resolve", handlers.MibResolve)
		r.With(RequireAdmin).Post("/mibs/upload", handlers.MibUpload)
		r.With(RequireAdmin).Post("/mibs/delete", handlers.MibDelete)

		r.Get("/topology/lldp", handlers.LldpTopologyPage)
		r.Get("/topology/lldp/data", handlers.LldpTopologyData)

		r.Route("/traps", func(r chi.Router) {
			r.Get("/page", handlers.TrapsPage)
			r.Get("/", handlers.ListTraps)
		})

		r.Get("/events/availability/page", handlers.AvailabilityEventsPage)
		r.Get("/events/availability", handlers.ListAvailabilityEvents)

		r.Get("/api/openapi.yaml", serveOpenAPISpec)

		r.With(RequireAdmin).Post("/test-alert", handlers.testAlert)
	})

	return r
}
