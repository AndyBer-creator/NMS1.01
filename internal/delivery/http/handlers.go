package http

import (
	"encoding/json"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"NMS1/internal/domain"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/repository"
	"NMS1/internal/usecases/discovery"

	"github.com/go-chi/chi/v5"
	"github.com/gosnmp/gosnmp"
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
)

type Handlers struct {
	repo             *postgres.Repo
	snmp             *snmp.Client
	scanner          *discovery.Scanner
	TrapsRepo        *repository.TrapsRepo
	logger           *zap.Logger
	devicesTableTmpl *template.Template
}

func NewHandlers(repo *postgres.Repo, snmpClient *snmp.Client, scanner *discovery.Scanner, trapsRepo *repository.TrapsRepo, logger *zap.Logger) *Handlers {
	devicesTableTmpl := template.Must(template.ParseFiles("templates/devices_table.html"))

	h := &Handlers{
		repo:             repo,
		snmp:             snmpClient,
		scanner:          scanner,
		TrapsRepo:        trapsRepo,
		logger:           logger,
		devicesTableTmpl: devicesTableTmpl,
	}
	return h
}

func init() {
	prometheus.MustRegister(requestsTotal)
}

func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, "templates/dashboard.html")
}

// ✅ GET /devices → JSON список устройств
func (h *Handlers) ListDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	devices, err := h.repo.ListDevices()
	if err != nil {
		h.logger.Error("ListDevices failed", zap.Error(err))
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(devices)
}

// ✅ /devices/table → HTML fragment для HTMX Dashboard
func (h *Handlers) DevicesTable(w http.ResponseWriter, r *http.Request) {
	devices, err := h.repo.ListDevices()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	type deviceRow struct {
		IP         string
		Name       string
		Status     string
		StatusClass string
		StatusIcon string
		LastSeen   string
	}
	type viewModel struct {
		Devices []deviceRow
		Total   int
	}

	rows := make([]deviceRow, 0, len(devices))
	for _, d := range devices {
		statusClass := "bg-red-500/20 text-red-400 border-red-500/50"
		statusIcon := "🔴"
		if d.Status == "active" {
			statusClass = "bg-green-500/20 text-green-400 border-green-500/50"
			statusIcon = "🟢"
		}

		lastSeen := "Никогда"
		if !d.LastSeen.IsZero() {
			lastSeen = d.LastSeen.Format("15:04 02.01")
		}

		rows = append(rows, deviceRow{
			IP:          d.IP,
			Name:        d.Name,
			Status:      d.Status,
			StatusClass: statusClass,
			StatusIcon:  statusIcon,
			LastSeen:    lastSeen,
		})
	}

	if err := h.devicesTableTmpl.Execute(w, viewModel{
		Devices: rows,
		Total:   len(rows),
	}); err != nil {
		http.Error(w, "Template render error", http.StatusInternalServerError)
		return
	}
}

// ✅ /devices/page → Отладочная страница
func (h *Handlers) DevicesPage(w http.ResponseWriter, r *http.Request) {
	devices, err := h.repo.ListDevices()
	if err != nil {
		http.Error(w, "DB error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "✅ DB OK! Devices: %d\n\n", len(devices))
	for _, dPtr := range devices {
		if dPtr == nil {
			continue
		}
		d := *dPtr
		fmt.Fprintf(w, "ID:%d | %s (%s) | %s\n", d.ID, d.Name, d.IP, d.Status)
	}
}

// SNMP метрика /devices/{ip}/metric/{oid}

func (h *Handlers) GetMetric(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	oid := chi.URLParam(r, "oid")

	device, err := h.repo.GetDeviceByIP(ip)
	if err != nil {
		h.logger.Error("GetDeviceByIP failed", zap.String("ip", ip), zap.Error(err))
		http.Error(w, "DB error", http.StatusInternalServerError)
		return
	}

	if device == nil {
		if demo := h.demoData(ip, oid); demo != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(demo)
			return
		}
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	result, err := h.snmp.GetDevice(device, []string{oid})
	if err != nil {
		h.logger.Error("SNMP Get failed", zap.String("ip", ip), zap.String("oid", oid), zap.Error(err))
		http.Error(w, "SNMP failed: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	h.repo.SaveMetric(device.ID, oid, result[oid])
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ✅ POST /devices/{ip}/snmp/set → SNMP SET (v2c/v3) для одного OID
func (h *Handlers) SetSNMP(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	if ip == "" {
		http.Error(w, "IP required", http.StatusBadRequest)
		return
	}

	var input struct {
		OID   string          `json:"oid"`
		Type  string          `json:"type"`  // Integer/OctetString/Counter64/...
		Value json.RawMessage `json:"value"` // тип зависит от Type
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if input.OID == "" || input.Type == "" {
		http.Error(w, "oid and type are required", http.StatusBadRequest)
		return
	}

	pduType, value, err := parseSNMPSetRequest(input.Type, input.Value)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	device, err := h.repo.GetDeviceByIP(ip)
	if err != nil {
		h.logger.Error("GetDeviceByIP failed", zap.String("ip", ip), zap.Error(err))
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if device == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	if err := h.snmp.SetDevice(device, input.OID, pduType, value); err != nil {
		h.logger.Error("SNMP SET failed", zap.String("ip", ip), zap.String("oid", input.OID), zap.String("type", input.Type), zap.Error(err))
		http.Error(w, "SNMP SET failed: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"ip":     ip,
		"oid":    input.OID,
	})
}

func parseSNMPSetRequest(typeStr string, raw json.RawMessage) (gosnmp.Asn1BER, interface{}, error) {
	t := strings.ToLower(strings.TrimSpace(typeStr))

	switch t {
	case "null":
		return gosnmp.Null, nil, nil
	case "integer":
		var i int
		if err := json.Unmarshal(raw, &i); err == nil {
			return gosnmp.Integer, i, nil
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, nil, fmt.Errorf("integer value must be a number or string")
		}
		parsed, err := strconv.Atoi(s)
		if err != nil {
			return 0, nil, fmt.Errorf("invalid integer value: %v", err)
		}
		return gosnmp.Integer, parsed, nil

	case "octetstring":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, nil, fmt.Errorf("octetstring value must be a string")
		}
		return gosnmp.OctetString, s, nil

	case "counter64":
		var u uint64
		if err := json.Unmarshal(raw, &u); err != nil {
			var s string
			if err2 := json.Unmarshal(raw, &s); err2 != nil {
				return 0, nil, fmt.Errorf("counter64 value must be a number or string")
			}
			parsed, err2 := strconv.ParseUint(s, 10, 64)
			if err2 != nil {
				return 0, nil, fmt.Errorf("invalid counter64 value: %v", err2)
			}
			u = parsed
		}
		return gosnmp.Counter64, u, nil

	case "counter32":
		var u uint64
		if err := json.Unmarshal(raw, &u); err != nil {
			var s string
			if err2 := json.Unmarshal(raw, &s); err2 != nil {
				return 0, nil, fmt.Errorf("counter32 value must be a number or string")
			}
			parsed, err2 := strconv.ParseUint(s, 10, 64)
			if err2 != nil {
				return 0, nil, fmt.Errorf("invalid counter32 value: %v", err2)
			}
			u = parsed
		}
		if u > uint64(^uint32(0)) {
			return 0, nil, fmt.Errorf("counter32 overflow")
		}
		return gosnmp.Counter32, uint32(u), nil

	case "gauge32":
		var u uint64
		if err := json.Unmarshal(raw, &u); err != nil {
			var s string
			if err2 := json.Unmarshal(raw, &s); err2 != nil {
				return 0, nil, fmt.Errorf("gauge32 value must be a number or string")
			}
			parsed, err2 := strconv.ParseUint(s, 10, 64)
			if err2 != nil {
				return 0, nil, fmt.Errorf("invalid gauge32 value: %v", err2)
			}
			u = parsed
		}
		if u > uint64(^uint32(0)) {
			return 0, nil, fmt.Errorf("gauge32 overflow")
		}
		return gosnmp.Gauge32, uint32(u), nil

	case "uinteger32":
		var u uint64
		if err := json.Unmarshal(raw, &u); err != nil {
			var s string
			if err2 := json.Unmarshal(raw, &s); err2 != nil {
				return 0, nil, fmt.Errorf("uinteger32 value must be a number or string")
			}
			parsed, err2 := strconv.ParseUint(s, 10, 64)
			if err2 != nil {
				return 0, nil, fmt.Errorf("invalid uinteger32 value: %v", err2)
			}
			u = parsed
		}
		if u > uint64(^uint32(0)) {
			return 0, nil, fmt.Errorf("uinteger32 overflow")
		}
		return gosnmp.Uinteger32, uint32(u), nil

	case "timeticks":
		var u uint64
		if err := json.Unmarshal(raw, &u); err != nil {
			var s string
			if err2 := json.Unmarshal(raw, &s); err2 != nil {
				return 0, nil, fmt.Errorf("timeticks value must be a number or string")
			}
			parsed, err2 := strconv.ParseUint(s, 10, 64)
			if err2 != nil {
				return 0, nil, fmt.Errorf("invalid timeticks value: %v", err2)
			}
			u = parsed
		}
		if u > uint64(^uint32(0)) {
			return 0, nil, fmt.Errorf("timeticks overflow")
		}
		return gosnmp.TimeTicks, uint32(u), nil

	case "ipaddress":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, nil, fmt.Errorf("ipaddress value must be a string (IPv4)")
		}
		return gosnmp.IPAddress, s, nil

	case "objectidentifier":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, nil, fmt.Errorf("objectidentifier value must be a string OID")
		}
		return gosnmp.ObjectIdentifier, s, nil

	default:
		return 0, nil, fmt.Errorf("unsupported snmp SET type: %s", typeStr)
	}
}

// ✅ POST /devices → Создать устройство
func (h *Handlers) CreateDevice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		IP          string `json:"ip" form:"ip"`
		Name        string `json:"name" form:"name"`
		Community   string `json:"community" form:"community"`
		SNMPVersion string `json:"snmp_version,omitempty" form:"snmp_version"`
		AuthProto   string `json:"auth_proto,omitempty" form:"auth_proto"`
		AuthPass    string `json:"auth_pass,omitempty" form:"auth_pass"`
		PrivProto   string `json:"priv_proto,omitempty" form:"priv_proto"`
		PrivPass    string `json:"priv_pass,omitempty" form:"priv_pass"`
	}

	// HTMX формы по умолчанию отправляют application/x-www-form-urlencoded,
	// поэтому сначала пробуем разобрать как form. JSON поддерживаем отдельно.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Cannot parse form", http.StatusBadRequest)
			return
		}
		input.IP = r.FormValue("ip")
		input.Name = r.FormValue("name")
		input.Community = r.FormValue("community")
		input.SNMPVersion = r.FormValue("snmp_version")
		input.AuthProto = r.FormValue("auth_proto")
		input.AuthPass = r.FormValue("auth_pass")
		input.PrivProto = r.FormValue("priv_proto")
		input.PrivPass = r.FormValue("priv_pass")
	}

	if input.IP == "" || input.Name == "" || input.Community == "" {
		http.Error(w, "IP, Name, Community required", http.StatusBadRequest)
		return
	}

	if strings.EqualFold(input.SNMPVersion, "v3") {
		// Для SNMPv3 используем `community` как UserName (из-за текущей схемы БД).
		if input.AuthProto == "" || input.AuthPass == "" {
			http.Error(w, "For snmp_version=v3 require auth_proto and auth_pass", http.StatusBadRequest)
			return
		}
		if (input.PrivProto == "") != (input.PrivPass == "") {
			http.Error(w, "For snmp_version=v3 require both priv_proto and priv_pass (or neither)", http.StatusBadRequest)
			return
		}
	}

	device := &domain.Device{
		IP:          input.IP,
		Name:        input.Name,
		Community:   input.Community,
		SNMPVersion: input.SNMPVersion,
		AuthProto:   input.AuthProto,
		AuthPass:    input.AuthPass,
		PrivProto:   input.PrivProto,
		PrivPass:    input.PrivPass,
	}

	if err := h.repo.CreateDevice(device); err != nil {
		h.logger.Error("CreateDevice failed", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     device.ID,
		"ip":     device.IP,
		"name":   device.Name,
		"status": "created",
	})
}

// ✅ DELETE /devices/{ip} → удалить устройство
func (h *Handlers) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	if ip == "" {
		http.Error(w, "IP required", http.StatusBadRequest)
		return
	}

	h.logger.Info("Deleting device", zap.String("ip", ip))

	if err := h.repo.DeleteByIP(ip); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Device not found", http.StatusNotFound)
			return
		}
		h.logger.Error("Delete failed", zap.String("ip", ip), zap.Error(err))
		http.Error(w, "Device deletion failed", http.StatusInternalServerError)
		return
	}

	// Возвращаем HTML-фрагмент таблицы, чтобы HTMX сразу обновил #devices-preview.
	// (HTMX вставляет ответ в целевой элемент.)
	h.DevicesTable(w, r)
}

// ✅ Health check
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// DiscoverScan — POST /discovery/scan: поиск SNMP-агентов в подсети (Get sysDescr).
func (h *Handlers) DiscoverScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		CIDR         string `json:"cidr"`
		Community    string `json:"community"`
		SNMPVersion  string `json:"snmp_version"`
		AuthProto    string `json:"auth_proto"`
		AuthPass     string `json:"auth_pass"`
		PrivProto    string `json:"priv_proto"`
		PrivPass     string `json:"priv_pass"`
		AutoAdd      *bool  `json:"auto_add"`
		TCPPrefilter *bool  `json:"tcp_prefilter"`
		Concurrency  int    `json:"concurrency"`
		MaxHosts     int    `json:"max_hosts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.CIDR) == "" {
		http.Error(w, "cidr is required", http.StatusBadRequest)
		return
	}

	autoAdd := false
	if body.AutoAdd != nil {
		autoAdd = *body.AutoAdd
	}
	tcpPref := false
	if body.TCPPrefilter != nil {
		tcpPref = *body.TCPPrefilter
	}

	params := discovery.ScanParams{
		CIDR:         body.CIDR,
		Community:    body.Community,
		SNMPVersion:  body.SNMPVersion,
		AuthProto:    body.AuthProto,
		AuthPass:     body.AuthPass,
		PrivProto:    body.PrivProto,
		PrivPass:     body.PrivPass,
		AutoAdd:      autoAdd,
		TCPPrefilter: tcpPref,
		Concurrency:  body.Concurrency,
		MaxHosts:     body.MaxHosts,
	}

	res, err := h.scanner.ScanNetwork(r.Context(), params)
	if err != nil {
		var se *discovery.ScanError
		if errors.As(err, &se) {
			http.Error(w, se.Error(), http.StatusBadRequest)
			return
		}
		h.logger.Error("DiscoverScan failed", zap.Error(err))
		http.Error(w, "Scan failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// ✅ Demo данные для тестов
func (h *Handlers) demoData(ip, oid string) map[string]string {
	demo := map[string]map[string]string{
		"192.168.0.1": {"1.3.6.1.2.1.1.1.0": "Keenetic Giga KN-1010 v3.7 (demo)"},
		"127.0.0.1":   {"1.3.6.1.2.1.1.1.0": "Ubuntu 24.04 LTS (demo)"},
	}
	if data, ok := demo[ip]; ok {
		if val, ok := data[oid]; ok {
			return map[string]string{oid: val}
		}
	}
	return nil
}
