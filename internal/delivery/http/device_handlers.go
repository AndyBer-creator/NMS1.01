package http

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"NMS1/internal/domain"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/mibresolver"

	"github.com/go-chi/chi/v5"
	"github.com/gosnmp/gosnmp"
	"go.uber.org/zap"
)

type devicesTableRow struct {
	ID int
	// IPHost is a URL-safe host (IPv6 wrapped in brackets).
	IPHost      string
	IP          string
	Name        string
	Status      string
	StatusClass string
	StatusIcon  string
	LastSeen    string
	LastPollOK  string
	LastError   string
	Admin       bool
}

type devicesTableViewModel struct {
	Devices   []devicesTableRow
	Total     int
	Admin     bool
	CSRFToken string
	CSPNonce  string
}

var allowedSNMPSetOIDs = map[string]struct{}{
	"1.3.6.1.2.1.2.2.1.7":     {},
	"1.3.6.1.2.1.2.2.1.5":     {},
	"1.3.6.1.2.1.31.1.1.1.18": {},
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

func ipHostForURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return s
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	if !addr.Is6() {
		return s
	}
	if z := addr.Zone(); z != "" {
		z = strings.ReplaceAll(z, "%", "%25")
		i := strings.LastIndexByte(s, '%')
		if i <= 0 {
			return "[" + addr.String() + "]"
		}
		base := strings.TrimSpace(s[:i])
		baseAddr, err := netip.ParseAddr(base)
		if err != nil || !baseAddr.Is6() {
			return "[" + addr.String() + "]"
		}
		return "[" + baseAddr.String() + "%25" + z + "]"
	}
	return "[" + addr.String() + "]"
}

func normalizeSNMPVersionInput(v string) (string, error) {
	return domain.NormalizeSNMPVersion(v)
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
			IPHost:      ipHostForURL(d.IP),
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

func (h *Handlers) ListDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	devices, err := h.repo.ListDevices(r.Context())
	if err != nil {
		h.logger.Error("ListDevices failed", zap.Error(err))
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(devices)
}

func (h *Handlers) DevicesTable(w http.ResponseWriter, r *http.Request) {
	devices, err := h.repo.ListDevices(r.Context())
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	vm := devicesTableViewModelFromDevices(devices)
	u := userFromContext(r.Context())
	vm.Admin = u != nil && u.role == roleAdmin
	vm.CSRFToken = csrfTokenFromContext(r)
	for i := range vm.Devices {
		vm.Devices[i].Admin = vm.Admin
	}
	if err := h.devicesTmpl.ExecuteTemplate(w, "devicesTable", vm); err != nil {
		http.Error(w, "Template render error", http.StatusInternalServerError)
		return
	}
}

func (h *Handlers) EditDeviceRow(w http.ResponseWriter, r *http.Request) {
	id, err := deviceIDFromChi(r)
	if err != nil {
		http.Error(w, "Invalid device id", http.StatusBadRequest)
		return
	}
	device, err := h.repo.GetDeviceByID(r.Context(), id)
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
	id, err := deviceIDFromChi(r)
	if err != nil {
		http.Error(w, "Invalid device id", http.StatusBadRequest)
		return
	}
	existing, err := h.repo.GetDeviceByID(r.Context(), id)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	if existing == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Cannot parse form", http.StatusBadRequest)
		return
	}

	patch := &domain.Device{
		IP:          existing.IP,
		Name:        strings.TrimSpace(r.FormValue("name")),
		Community:   strings.TrimSpace(r.FormValue("community")),
		SNMPVersion: r.FormValue("snmp_version"),
		AuthProto:   strings.TrimSpace(r.FormValue("auth_proto")),
		AuthPass:    r.FormValue("auth_pass"),
		PrivProto:   strings.TrimSpace(r.FormValue("priv_proto")),
		PrivPass:    r.FormValue("priv_pass"),
	}
	if strings.TrimSpace(r.FormValue("community")) == "" {
		patch.Community = existing.Community
	}
	if r.FormValue("auth_pass") == "" {
		patch.AuthPass = existing.AuthPass
	}
	if r.FormValue("priv_pass") == "" {
		patch.PrivPass = existing.PrivPass
	}
	if strings.TrimSpace(r.FormValue("auth_proto")) == "" {
		patch.AuthProto = existing.AuthProto
	}
	if strings.TrimSpace(r.FormValue("priv_proto")) == "" {
		patch.PrivProto = existing.PrivProto
	}

	snmpVer, verr := normalizeSNMPVersionInput(patch.SNMPVersion)
	if verr != nil {
		http.Error(w, verr.Error(), http.StatusBadRequest)
		return
	}
	patch.SNMPVersion = snmpVer

	if patch.Name == "" {
		http.Error(w, "Name required", http.StatusBadRequest)
		return
	}
	switch snmpVer {
	case "v1", "v2c":
		if strings.TrimSpace(patch.Community) == "" {
			http.Error(w, "Community required for SNMP v1/v2c", http.StatusBadRequest)
			return
		}
	case "v3":
		if strings.TrimSpace(patch.AuthProto) == "" || patch.AuthPass == "" {
			http.Error(w, "For SNMPv3 require auth_proto and auth_pass (или оставьте пустыми пароли, чтобы не менять)", http.StatusBadRequest)
			return
		}
		pp := strings.TrimSpace(patch.PrivProto)
		ppp := patch.PrivPass
		if (pp == "") != (ppp == "") {
			http.Error(w, "For SNMPv3 require both priv_proto and priv_pass (or neither)", http.StatusBadRequest)
			return
		}
	}

	updated, err := h.repo.UpdateDeviceByID(r.Context(), id, patch)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Device not found", http.StatusNotFound)
			return
		}
		h.logger.Error("UpdateDevice failed", zap.Int("id", id), zap.Error(err))
		http.Error(w, "Update failed", http.StatusInternalServerError)
		return
	}
	if updated == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}
	devicesVM := devicesTableViewModelFromDevices([]*domain.Device{updated})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.devicesTmpl.ExecuteTemplate(w, "deviceRow", devicesVM.Devices[0])
}

func (h *Handlers) DevicesListPage(w http.ResponseWriter, r *http.Request) {
	devices, err := h.repo.ListDevices(r.Context())
	if err != nil {
		h.logger.Error("DevicesListPage failed", zap.Error(err))
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	vm := devicesTableViewModelFromDevices(devices)
	u := userFromContext(r.Context())
	vm.Admin = u != nil && u.role == roleAdmin
	vm.CSPNonce = cspNonceFromContext(r)
	for i := range vm.Devices {
		vm.Devices[i].Admin = vm.Admin
	}
	if err := h.devicesTmpl.ExecuteTemplate(w, "devicesPage", vm); err != nil {
		h.logger.Error("DevicesListPage template", zap.Error(err))
		http.Error(w, "Template render error", http.StatusInternalServerError)
		return
	}
}

func (h *Handlers) GetMetric(w http.ResponseWriter, r *http.Request) {
	h.syncSNMPRuntimeConfig(r.Context())
	id, err := deviceIDFromChi(r)
	if err != nil {
		http.Error(w, "Invalid device id", http.StatusBadRequest)
		return
	}
	oid := chi.URLParam(r, "oid")

	numericOID, err := h.resolveOIDInput(oid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	device, err := h.repo.GetDeviceByID(r.Context(), id)
	if err != nil {
		h.logger.Error("GetDeviceByID failed", zap.Int("id", id), zap.Error(err))
		http.Error(w, "DB error", http.StatusInternalServerError)
		return
	}
	if device == nil {
		http.Error(w, "Device not found", http.StatusNotFound)
		return
	}

	result, err := h.snmp.GetDevice(device, []string{numericOID})
	if err != nil {
		h.logger.Error("SNMP Get failed", zap.String("ip", device.IP), zap.String("oid", numericOID), zap.Error(err))
		http.Error(w, "SNMP failed", http.StatusServiceUnavailable)
		return
	}

	val := mibresolver.PickSNMPValue(result, numericOID)
	if err := h.repo.SaveMetric(r.Context(), device.ID, numericOID, val); err != nil {
		h.logger.Warn("SaveMetric", zap.Error(err))
	}

	out := map[string]string{numericOID: val}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (h *Handlers) SetSNMP(w http.ResponseWriter, r *http.Request) {
	h.syncSNMPRuntimeConfig(r.Context())
	id, err := deviceIDFromChi(r)
	if err != nil {
		h.writeAPIError(w, http.StatusBadRequest, "validation_error", "invalid device id")
		return
	}

	var input struct {
		OID          string          `json:"oid"`
		Type         string          `json:"type"`
		Value        json.RawMessage `json:"value"`
		ValidateOnly bool            `json:"validate_only"`
	}
	if err := decodeJSONBody(w, r, &input); err != nil {
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

	device, err := h.repo.GetDeviceByID(r.Context(), id)
	if err != nil {
		h.logger.Error("GetDeviceByID failed", zap.Int("id", id), zap.Error(err))
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
		_ = h.repo.InsertSNMPSetAudit(r.Context(), postgres.SNMPSetAuditRecord{
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
			"id":            device.ID,
			"ip":            device.IP,
			"oid":           numericOID,
			"type":          input.Type,
			"validate_only": true,
		})
		return
	}

	if err := h.snmp.SetDevice(device, numericOID, pduType, value); err != nil {
		h.logger.Error("SNMP SET failed", zap.String("ip", device.IP), zap.String("oid", numericOID), zap.String("type", input.Type), zap.Error(err))
		_ = h.repo.InsertSNMPSetAudit(r.Context(), postgres.SNMPSetAuditRecord{
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

	_ = h.repo.InsertSNMPSetAudit(r.Context(), postgres.SNMPSetAuditRecord{
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
		"ip":     device.IP,
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

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		if err := decodeJSONBody(w, r, &input); err != nil {
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
	if err := h.repo.CreateDevice(r.Context(), device); err != nil {
		h.logger.Error("CreateDevice failed", zap.Error(err))
		http.Error(w, "device creation failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     device.ID,
		"ip":     device.IP,
		"name":   device.Name,
		"status": "created",
	})
}

func (h *Handlers) DeleteDevice(w http.ResponseWriter, r *http.Request) {
	id, err := deviceIDFromChi(r)
	if err != nil {
		http.Error(w, "Invalid device id", http.StatusBadRequest)
		return
	}

	h.logger.Info("Deleting device", zap.Int("id", id))
	if err := h.repo.DeleteByID(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Device not found", http.StatusNotFound)
			return
		}
		h.logger.Error("Delete failed", zap.Int("id", id), zap.Error(err))
		http.Error(w, "Device deletion failed", http.StatusInternalServerError)
		return
	}
	h.DevicesTable(w, r)
}
