package http

import (
	"context"
	"NMS1/internal/usecases/discovery"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// DiscoverScan — POST /discovery/scan: поиск SNMP-агентов в подсети (Get sysDescr).
func (h *Handlers) DiscoverScan(w http.ResponseWriter, r *http.Request) {
	h.syncSNMPRuntimeConfig(r.Context())
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
	if err := decodeJSONBody(w, r, &body); err != nil {
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
		if errors.Is(err, context.Canceled) {
			h.logger.Info("DiscoverScan canceled by client")
			return
		}
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
