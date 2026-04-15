package http

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Router возвращает корневой HTTP-handler: WebSocket терминала обслуживается отдельной
// цепочкой без Prometheus/Logger, чтобы Hijack всегда доходил до net.Conn (иначе браузер: 1006).
func Router(handlers *Handlers) http.Handler {
	main := mainRouter(handlers)
	term := terminalWSRouter(handlers)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/ws/terminal/") {
			term.ServeHTTP(w, r)
			return
		}
		main.ServeHTTP(w, r)
	})
}

func terminalWSRouter(handlers *Handlers) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(SecurityHeaders)
	r.Use(EnforceHTTPS)
	r.Get("/terminal/{id}", handlers.TerminalWS)
	return http.StripPrefix("/ws", r)
}

func mainRouter(handlers *Handlers) *chi.Mux {
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
	r.Post("/itsm/inbound", handlers.ITSMInboundWebhook)
	r.Post("/itsm/inbound/dry-run", handlers.ITSMInboundDryRun)

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

		r.With(RequireAdmin).Post("/discovery/scan", handlers.DiscoverScan)

		r.Get("/settings/worker-poll-panel", handlers.WorkerPollSettingsPanel)
		r.Get("/settings/incident-automation-panel", handlers.IncidentAutomationPanel)
		r.Get("/settings/incident-automation-snapshot", handlers.IncidentAutomationSnapshot)
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
		r.Get("/trap-oid-mappings/page", handlers.TrapOIDMappingsPage)
		r.Get("/trap-oid-mappings", handlers.ListTrapOIDMappings)
		r.With(RequireAdmin).Post("/trap-oid-mappings", handlers.CreateTrapOIDMapping)
		r.With(RequireAdmin).Put("/trap-oid-mappings", handlers.UpdateTrapOIDMapping)
		r.With(RequireAdmin).Delete("/trap-oid-mappings", handlers.DeleteTrapOIDMapping)
		r.Get("/itsm-inbound-mappings/page", handlers.ITSMInboundMappingsPage)
		r.Get("/itsm-inbound-mappings", handlers.ListITSMInboundMappings)
		r.With(RequireAdmin).Post("/itsm-inbound-mappings", handlers.CreateITSMInboundMapping)
		r.With(RequireAdmin).Put("/itsm-inbound-mappings", handlers.UpdateITSMInboundMapping)
		r.With(RequireAdmin).Delete("/itsm-inbound-mappings", handlers.DeleteITSMInboundMapping)

		r.Get("/events/availability/page", handlers.AvailabilityEventsPage)
		r.Get("/events/availability", handlers.ListAvailabilityEvents)

		r.Get("/incidents", handlers.ListIncidents)
		r.Get("/incidents/page", handlers.IncidentsPage)
		r.Get("/incidents/{incidentID}", handlers.GetIncident)
		r.With(RequireAdmin).Post("/incidents", handlers.CreateIncident)
		r.With(RequireAdmin).Post("/incidents/{incidentID}/status", handlers.TransitionIncident)
		r.With(RequireAdmin).Post("/incidents/{incidentID}/assignee", handlers.AssignIncident)
		r.With(RequireAdmin).Post("/incidents/bulk/status", handlers.BulkTransitionIncidents)

		r.Get("/api/openapi.yaml", serveOpenAPISpec)

		r.With(RequireAdmin).Post("/test-alert", handlers.testAlert)
	})

	return r
}
