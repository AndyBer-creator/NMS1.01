package http

import (
	"html/template"
	"net/http"
	"time"

	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/mibresolver"
	"NMS1/internal/repository"
	"NMS1/internal/usecases/discovery"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

var (
	requestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nms_requests_total",
			Help: "Total NMS API requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	requestDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nms_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint", "status"},
	)
	incidentsCreatedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nms_incidents_created_total",
			Help: "Total incidents created via API",
		},
		[]string{"source", "severity"},
	)
	incidentTransitionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nms_incident_transitions_total",
			Help: "Total incident status transitions via API",
		},
		[]string{"from_status", "to_status", "source", "severity"},
	)
	incidentAckLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nms_incident_ack_latency_seconds",
			Help:    "Time from incident creation to acknowledge",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"source", "severity"},
	)
	incidentResolveLatencySeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nms_incident_resolve_latency_seconds",
			Help:    "Time from incident creation to resolve/close",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"source", "severity", "final_status"},
	)
)

// Handlers groups HTTP handlers and shared dependencies/templates.
type Handlers struct {
	repo                        *postgres.Repo
	snmp                        *snmp.Client
	scanner                     *discovery.Scanner
	trapHTTPRepo                repository.TrapHTTPRepository
	logger                      *zap.Logger
	dashboardTmpl               *template.Template
	devicesTmpl                 *template.Template // devicesTable + devicesPage
	mibPanelTmpl                *template.Template
	loginTmpl                   *template.Template
	terminalTmpl                *template.Template
	trapsPageTmpl               *template.Template
	eventsPageTmpl              *template.Template
	incidentsPageTmpl           *template.Template
	trapOIDMappingsPageTmpl     *template.Template
	itsmInboundMappingsPageTmpl *template.Template
	topologyTmpl                *template.Template
	mibUploadDir                string
	mib                         *mibresolver.Resolver
	httpClient                  *http.Client
}

// NewHandlers wires HTTP handlers with repositories, services and templates.
func NewHandlers(repo *postgres.Repo, snmpClient *snmp.Client, scanner *discovery.Scanner, trapsRepo repository.TrapHTTPRepository, logger *zap.Logger, mibUploadDir string, mib *mibresolver.Resolver) *Handlers {
	setSessionRevocationStore(repo)
	dashboardTmpl := template.Must(template.ParseFiles("templates/dashboard.html"))
	devicesTmpl := template.Must(template.ParseFiles(
		"templates/devices_table.html",
		"templates/devices_page.html",
		"templates/worker_poll_panel.html",
		"templates/snmp_runtime_panel.html",
		"templates/alert_email_panel.html",
		"templates/runtime_settings_panel.html",
		"templates/secret_settings_panel.html",
		"templates/settings_page.html",
	))
	mibPanelTmpl := template.Must(template.ParseFiles("templates/mibs_panel.html"))
	loginTmpl := template.Must(template.ParseFiles("templates/login.html"))
	terminalTmpl := template.Must(template.ParseFiles("templates/device_terminal.html"))
	trapsPageTmpl := template.Must(template.ParseFiles("templates/traps_page.html"))
	eventsPageTmpl := template.Must(template.ParseFiles("templates/events_availability.html"))
	incidentsPageTmpl := template.Must(template.ParseFiles("templates/incidents_page.html"))
	trapOIDMappingsPageTmpl := template.Must(template.ParseFiles("templates/trap_oid_mappings_page.html"))
	itsmInboundMappingsPageTmpl := template.Must(template.ParseFiles("templates/itsm_inbound_mappings_page.html"))
	topologyTmpl := template.Must(template.ParseFiles("templates/topology_lldp_page.html"))

	h := &Handlers{
		repo:                        repo,
		snmp:                        snmpClient,
		scanner:                     scanner,
		trapHTTPRepo:                trapsRepo,
		logger:                      logger,
		dashboardTmpl:               dashboardTmpl,
		devicesTmpl:                 devicesTmpl,
		mibPanelTmpl:                mibPanelTmpl,
		loginTmpl:                   loginTmpl,
		terminalTmpl:                terminalTmpl,
		trapsPageTmpl:               trapsPageTmpl,
		eventsPageTmpl:              eventsPageTmpl,
		incidentsPageTmpl:           incidentsPageTmpl,
		trapOIDMappingsPageTmpl:     trapOIDMappingsPageTmpl,
		itsmInboundMappingsPageTmpl: itsmInboundMappingsPageTmpl,
		topologyTmpl:                topologyTmpl,
		mibUploadDir:                mibUploadDir,
		mib:                         mib,
		httpClient:                  &http.Client{Timeout: 1200 * time.Millisecond},
	}
	return h
}

func init() {
	prometheus.MustRegister(requestsTotal)
	prometheus.MustRegister(requestDurationSeconds)
	prometheus.MustRegister(incidentsCreatedTotal)
	prometheus.MustRegister(incidentTransitionsTotal)
	prometheus.MustRegister(incidentAckLatencySeconds)
	prometheus.MustRegister(incidentResolveLatencySeconds)
}
