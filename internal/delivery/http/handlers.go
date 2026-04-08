package http

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strconv"
	"strings"

	"NMS1/internal/domain"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/mibresolver"
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

	requestDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "nms_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
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
	devicesTmpl      *template.Template // devicesTable + devicesPage
	mibPanelTmpl     *template.Template
	mibUploadDir     string
	mib              *mibresolver.Resolver
}

func NewHandlers(repo *postgres.Repo, snmpClient *snmp.Client, scanner *discovery.Scanner, trapsRepo *repository.TrapsRepo, logger *zap.Logger, mibUploadDir string, mib *mibresolver.Resolver) *Handlers {
	devicesTmpl := template.Must(template.ParseFiles(
		"templates/devices_table.html",
		"templates/devices_page.html",
	))
	mibPanelTmpl := template.Must(template.ParseFiles("templates/mibs_panel.html"))

	h := &Handlers{
		repo:         repo,
		snmp:         snmpClient,
		scanner:      scanner,
		TrapsRepo:    trapsRepo,
		logger:       logger,
		devicesTmpl:  devicesTmpl,
		mibPanelTmpl: mibPanelTmpl,
		mibUploadDir: mibUploadDir,
		mib:          mib,
	}
	return h
}

// resolveOIDInput: числовой OID как есть; иначе snmptranslate (net-snmp-tools, MIBDIRS из конфига).
func (h *Handlers) resolveOIDInput(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("пустой OID")
	}
	return h.mib.ResolveToNumeric(s)
}

func init() {
	prometheus.MustRegister(requestsTotal)
	prometheus.MustRegister(requestDurationSeconds)
}

type devicesTableRow struct {
	ID          int
	IP          string
	Name        string
	Status      string
	StatusClass string
	StatusIcon  string
	LastSeen    string
	LastPollOK  string
	LastError   string
}

type devicesTableViewModel struct {
	Devices []devicesTableRow
	Total   int
}

var allowedSNMPSetOIDs = map[string]struct{}{
	"1.3.6.1.2.1.2.2.1.7":  {}, // ifAdminStatus
	"1.3.6.1.2.1.2.2.1.5":  {}, // ifSpeed (device support dependent)
	"1.3.6.1.2.1.31.1.1.1.18": {}, // ifAlias
}

func isAllowedSetOID(numericOID string) bool {
	numericOID = strings.TrimPrefix(strings.TrimSpace(numericOID), ".")
	for prefix := range allowedSNMPSetOIDs {
		if numericOID == prefix || strings.HasPrefix(numericOID, prefix+".") {
			return true
		}
	}
	return false
}

func devicesTableViewModelFromDevices(devices []*domain.Device) devicesTableViewModel {
	rows := make([]devicesTableRow, 0, len(devices))
	for _, d := range devices {
		if d == nil {
			continue
		}
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
		lastPollOK := "Нет успешного опроса"
		if !d.LastPollOKAt.IsZero() {
			lastPollOK = d.LastPollOKAt.Format("15:04 02.01")
		}
		lastError := "—"
		if strings.TrimSpace(d.LastError) != "" {
			lastError = d.LastError
			if !d.LastErrorAt.IsZero() {
				lastError = d.LastErrorAt.Format("15:04 02.01") + " — " + lastError
			}
		}

		rows = append(rows, devicesTableRow{
			ID:          d.ID,
			IP:          d.IP,
			Name:        d.Name,
			Status:      d.Status,
			StatusClass: statusClass,
			StatusIcon:  statusIcon,
			LastSeen:    lastSeen,
			LastPollOK:  lastPollOK,
			LastError:   lastError,
		})
	}
	return devicesTableViewModel{Devices: rows, Total: len(rows)}
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

	vm := devicesTableViewModelFromDevices(devices)
	if err := h.devicesTmpl.ExecuteTemplate(w, "devicesTable", vm); err != nil {
		http.Error(w, "Template render error", http.StatusInternalServerError)
		return
	}
}

func (h *Handlers) EditDeviceRow(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	if strings.TrimSpace(ip) == "" {
		http.Error(w, "IP required", http.StatusBadRequest)
		return
	}
	device, err := h.repo.GetDeviceByIP(ip)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if device == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "deviceRowEdit", device)
}

func (h *Handlers) UpdateDevice(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	if strings.TrimSpace(ip) == "" {
		http.Error(w, "IP required", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cannot parse form", http.StatusBadRequest)
		return
	}

	patch := &domain.Device{
		IP:          ip,
		Name:        r.FormValue("name"),
		Community:   r.FormValue("community"),
		SNMPVersion: r.FormValue("snmp_version"),
		AuthProto:   r.FormValue("auth_proto"),
		AuthPass:    r.FormValue("auth_pass"),
		PrivProto:   r.FormValue("priv_proto"),
		PrivPass:    r.FormValue("priv_pass"),
	}
	if strings.TrimSpace(patch.Name) == "" || strings.TrimSpace(patch.Community) == "" {
		http.Error(w, "Name and Community required", http.StatusBadRequest)
		return
	}
	if strings.EqualFold(patch.SNMPVersion, "v3") {
		if strings.TrimSpace(patch.AuthProto) == "" || patch.AuthPass == "" {
			http.Error(w, "For snmp_version=v3 require auth_proto and auth_pass", http.StatusBadRequest)
			return
		}
		if (strings.TrimSpace(patch.PrivProto) == "") != (patch.PrivPass == "") {
			http.Error(w, "For snmp_version=v3 require both priv_proto and priv_pass (or neither)", http.StatusBadRequest)
			return
		}
	}

	updated, err := h.repo.UpdateDeviceByIP(ip, patch)
	if err != nil {
		http.Error(w, "Update failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	devicesVM := devicesTableViewModelFromDevices([]*domain.Device{updated})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "deviceRow", devicesVM.Devices[0])
}

// DevicesListPage — полная HTML-страница таблицы устройств (дизайн как у дашборда).
func (h *Handlers) DevicesListPage(w http.ResponseWriter, r *http.Request) {
	devices, err := h.repo.ListDevices()
	if err != nil {
		h.logger.Error("DevicesListPage failed", zap.Error(err))
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	vm := devicesTableViewModelFromDevices(devices)
	if err := h.devicesTmpl.ExecuteTemplate(w, "devicesPage", vm); err != nil {
		h.logger.Error("DevicesListPage template", zap.Error(err))
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

	numericOID, err := h.resolveOIDInput(oid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	device, err := h.repo.GetDeviceByIP(ip)
	if err != nil {
		h.logger.Error("GetDeviceByIP failed", zap.String("ip", ip), zap.Error(err))
		http.Error(w, "DB error", http.StatusInternalServerError)
		return
	}

	if device == nil {
		if demo := h.demoData(ip, numericOID); demo != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(demo)
			return
		}
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	result, err := h.snmp.GetDevice(device, []string{numericOID})
	if err != nil {
		h.logger.Error("SNMP Get failed", zap.String("ip", ip), zap.String("oid", numericOID), zap.Error(err))
		http.Error(w, "SNMP failed: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	val := mibresolver.PickSNMPValue(result, numericOID)
	if err := h.repo.SaveMetric(device.ID, numericOID, val); err != nil {
		h.logger.Warn("SaveMetric", zap.Error(err))
	}

	out := map[string]string{numericOID: val}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// ✅ POST /devices/{ip}/snmp/set → SNMP SET (v2c/v3) для одного OID
func (h *Handlers) SetSNMP(w http.ResponseWriter, r *http.Request) {
	ip := chi.URLParam(r, "ip")
	if ip == "" {
		h.writeAPIError(w, http.StatusBadRequest, "validation_error", "ip is required")
		return
	}

	var input struct {
		OID          string          `json:"oid"`
		Type         string          `json:"type"`          // Integer/OctetString/Counter64/...
		Value        json.RawMessage `json:"value"`         // тип зависит от Type
		ValidateOnly bool            `json:"validate_only"` // только валидация, без SNMP SET
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		h.writeAPIError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}
	if input.OID == "" || input.Type == "" {
		h.writeAPIError(w, http.StatusBadRequest, "validation_error", "oid and type are required")
		return
	}

	numericOID, err := h.resolveOIDInput(input.OID)
	if err != nil {
		h.writeAPIError(w, http.StatusBadRequest, "invalid_oid", err.Error())
		return
	}
	if !isAllowedSetOID(numericOID) {
		h.writeAPIError(w, http.StatusForbidden, "oid_not_allowed", "OID is not allowed for SNMP SET")
		return
	}

	pduType, value, err := parseSNMPSetRequest(input.Type, input.Value)
	if err != nil {
		h.writeAPIError(w, http.StatusBadRequest, "invalid_value", err.Error())
		return
	}

	device, err := h.repo.GetDeviceByIP(ip)
	if err != nil {
		h.logger.Error("GetDeviceByIP failed", zap.String("ip", ip), zap.Error(err))
		h.writeAPIError(w, http.StatusInternalServerError, "db_error", "Database error")
		return
	}
	if device == nil {
		h.writeAPIError(w, http.StatusNotFound, "not_found", "Device not found")
		return
	}

	oldValue := ""
	if current, getErr := h.snmp.GetDevice(device, []string{numericOID}); getErr == nil {
		oldValue = mibresolver.PickSNMPValue(current, numericOID)
	}
	newValue := fmt.Sprintf("%v", value)
	username := "system"
	if u := userFromContext(r.Context()); u != nil && strings.TrimSpace(u.username) != "" {
		username = strings.TrimSpace(u.username)
	}

	if input.ValidateOnly {
		_ = h.repo.InsertSNMPSetAudit(postgres.SNMPSetAuditRecord{
			UserName: username,
			DeviceID: sql.NullInt64{Int64: int64(device.ID), Valid: true},
			OID:      numericOID,
			OldValue: oldValue,
			NewValue: newValue,
			Result:   "validated",
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "validated",
			"ip":            ip,
			"oid":           numericOID,
			"type":          input.Type,
			"validate_only": true,
		})
		return
	}

	if err := h.snmp.SetDevice(device, numericOID, pduType, value); err != nil {
		h.logger.Error("SNMP SET failed", zap.String("ip", ip), zap.String("oid", numericOID), zap.String("type", input.Type), zap.Error(err))
		_ = h.repo.InsertSNMPSetAudit(postgres.SNMPSetAuditRecord{
			UserName: username,
			DeviceID: sql.NullInt64{Int64: int64(device.ID), Valid: true},
			OID:      numericOID,
			OldValue: oldValue,
			NewValue: newValue,
			Result:   "failed",
			Error:    err.Error(),
		})
		h.writeAPIError(w, http.StatusServiceUnavailable, "snmp_set_failed", "SNMP SET failed: "+err.Error())
		return
	}

	_ = h.repo.InsertSNMPSetAudit(postgres.SNMPSetAuditRecord{
		UserName: username,
		DeviceID: sql.NullInt64{Int64: int64(device.ID), Valid: true},
		OID:      numericOID,
		OldValue: oldValue,
		NewValue: newValue,
		Result:   "ok",
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"ip":     ip,
		"oid":    numericOID,
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

// LldpTopologyPage отдает HTML страницу с графом LLDP.
func (h *Handlers) LldpTopologyPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, "templates/topology_lldp_page.html")
}

type topologyNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Color string `json:"color,omitempty"`
	Shape string `json:"shape,omitempty"`
}

type topologyEdge struct {
	ID    string `json:"id"`
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
	Title string `json:"title,omitempty"`
	Color string `json:"color,omitempty"`
}

type topologyResponse struct {
	ScanID int64            `json:"scan_id"`
	Nodes  []topologyNode  `json:"nodes"`
	Edges  []topologyEdge  `json:"edges"`
}

func hashShort(s string) string {
	sum := sha1.Sum([]byte(s))
	hexed := hex.EncodeToString(sum[:])
	if len(hexed) > 10 {
		return hexed[:10]
	}
	return hexed
}

// LldpTopologyData отдает JSON: узлы и ребра последнего LLDP-снимка.
func (h *Handlers) LldpTopologyData(w http.ResponseWriter, r *http.Request) {
	scanID, err := h.repo.GetLatestLldpScanID()
	if err != nil {
		http.Error(w, "Failed to load latest LLDP scan: "+err.Error(), http.StatusInternalServerError)
		return
	}

	links, err := h.repo.GetLatestLldpLinks()
	if err != nil {
		http.Error(w, "Failed to load LLDP links: "+err.Error(), http.StatusInternalServerError)
		return
	}

	nodesByID := make(map[string]topologyNode)
	var edges []topologyEdge

	for _, l := range links {
		if l.LocalIP == "" {
			continue
		}
		localKey := "dev:" + l.LocalIP
		localLabel := l.LocalIP
		if strings.TrimSpace(l.LocalName) != "" {
			localLabel = fmt.Sprintf("%s (%s)", l.LocalName, l.LocalIP)
		}
		nodesByID[localKey] = topologyNode{
			ID:    localKey,
			Label: localLabel,
			Color: "#60a5fa",
			Shape: "dot",
		}

		var remoteKey string
		var remoteLabel string
		remoteColor := "#f97316"
		remoteShape := "box"

		if l.RemoteIP != nil && strings.TrimSpace(*l.RemoteIP) != "" {
			remoteKey = "dev:" + *l.RemoteIP
			remoteLabel = *l.RemoteIP
			if strings.TrimSpace(l.RemoteName) != "" {
				remoteLabel = fmt.Sprintf("%s (%s)", l.RemoteName, *l.RemoteIP)
			}
			remoteColor = "#34d399"
			remoteShape = "dot"
		} else {
			base := strings.TrimSpace(l.RemoteSysName)
			if base == "" {
				base = strings.TrimSpace(l.RemoteSysDesc)
			}
			if base == "" {
				base = "Unknown neighbor"
			}
			remoteKey = "unk:" + hashShort(base+"|"+l.RemotePortID)
			remoteLabel = base
		}

		nodesByID[remoteKey] = topologyNode{
			ID:    remoteKey,
			Label: remoteLabel,
			Color: remoteColor,
			Shape: remoteShape,
		}

		remotePort := strings.TrimSpace(l.RemotePortDesc)
		if remotePort == "" {
			remotePort = strings.TrimSpace(l.RemotePortID)
		}
		localPort := strings.TrimSpace(l.LocalPortDesc)
		if localPort == "" {
			localPort = fmt.Sprintf("port-%d", l.LocalPortNum)
		}

		edgeLabel := localPort + " → " + remotePort
		edgeKey := localKey + "|" + localPort + "|" + remoteKey + "|" + remotePort
		edges = append(edges, topologyEdge{
			ID:    "e:" + hashShort(edgeKey),
			From:  localKey,
			To:    remoteKey,
			Label: edgeLabel,
			Title: edgeLabel,
		})
	}

	resp := topologyResponse{
		ScanID: scanID,
		Nodes:  make([]topologyNode, 0, len(nodesByID)),
		Edges:  edges,
	}
	for _, n := range nodesByID {
		resp.Nodes = append(resp.Nodes, n)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// DiscoverScan — POST /discovery/scan: поиск SNMP-агентов в подсети (Get sysDescr).
func (h *Handlers) DiscoverScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		h.writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
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
		h.writeAPIError(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}
	cidr := strings.TrimSpace(body.CIDR)
	if cidr == "" {
		h.writeAPIError(w, http.StatusBadRequest, "validation_error", "cidr is required")
		return
	}
	if _, _, err := net.ParseCIDR(cidr); err != nil {
		h.writeAPIError(w, http.StatusBadRequest, "validation_error", "invalid cidr format")
		return
	}
	if body.Concurrency < 0 || body.Concurrency > 512 {
		h.writeAPIError(w, http.StatusBadRequest, "validation_error", "concurrency must be in range 0..512")
		return
	}
	if body.MaxHosts < 0 || body.MaxHosts > 65536 {
		h.writeAPIError(w, http.StatusBadRequest, "validation_error", "max_hosts must be in range 0..65536")
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
		CIDR:         cidr,
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
			h.writeAPIError(w, http.StatusBadRequest, "scan_validation_error", se.Error())
			return
		}
		h.logger.Error("DiscoverScan failed", zap.Error(err))
		h.writeAPIError(w, http.StatusInternalServerError, "scan_failed", "Scan failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

type apiErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Status  int    `json:"status"`
	Details string `json:"details,omitempty"`
}

func (h *Handlers) writeAPIError(w http.ResponseWriter, status int, code, msg string) {
	if strings.TrimSpace(code) == "" {
		code = "unknown_error"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorResponse{
		Error:  msg,
		Code:   code,
		Status: status,
	})
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
