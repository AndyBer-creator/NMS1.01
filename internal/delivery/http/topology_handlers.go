package http

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

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
	ScanID int64          `json:"scan_id"`
	Nodes  []topologyNode `json:"nodes"`
	Edges  []topologyEdge `json:"edges"`
}

func hashShort(s string) string {
	sum := sha1.Sum([]byte(s))
	hexed := hex.EncodeToString(sum[:])
	if len(hexed) > 10 {
		return hexed[:10]
	}
	return hexed
}

// LldpTopologyPage renders LLDP topology graph page.
func (h *Handlers) LldpTopologyPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = h.topologyTmpl.Execute(w, map[string]any{"CSPNonce": cspNonceFromContext(r)})
}

// LldpTopologyData returns nodes/edges from latest LLDP snapshot.
func (h *Handlers) LldpTopologyData(w http.ResponseWriter, r *http.Request) {
	scanID, err := h.repo.GetLatestLldpScanID(r.Context())
	if err != nil {
		h.logger.Error("Failed to load latest LLDP scan", zap.Error(err))
		http.Error(w, "Failed to load latest LLDP scan", http.StatusInternalServerError)
		return
	}

	links, err := h.repo.GetLatestLldpLinks(r.Context())
	if err != nil {
		h.logger.Error("Failed to load LLDP links", zap.Error(err))
		http.Error(w, "Failed to load LLDP links", http.StatusInternalServerError)
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

		edgeLabel := localPort + " -> " + remotePort
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
