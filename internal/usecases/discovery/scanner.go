package discovery

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"NMS1/internal/domain"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"
	"NMS1/internal/mibresolver"

	"go.uber.org/zap"
)

const (
	sysDescrOID     = "1.3.6.1.2.1.1.1.0"
	defaultMaxHosts = 2048
	defaultWorkers  = 80
)

// ScanParams defines subnet discovery parameters for SNMP agent probing.
type ScanParams struct {
	CIDR         string
	Community    string // v2c community; v3 username (stored in DB community field)
	SNMPVersion  string
	AuthProto    string
	AuthPass     string
	PrivProto    string
	PrivPass     string
	AutoAdd      bool // auto-register discovered hosts in devices table
	TCPPrefilter bool // probe common TCP ports first (may skip SNMP-only hosts)
	Concurrency  int  // 0 → defaultWorkers
	MaxHosts     int  // 0 → defaultMaxHosts upper bound for host fanout
}

// FoundHost describes one host that successfully answered SNMP probe.
type FoundHost struct {
	IP       string `json:"ip"`
	SysDescr string `json:"sys_descr,omitempty"`
	Added    bool   `json:"added"` // inserted into DB when AutoAdd is enabled
}

// ScanResult contains discovery output and runtime metadata.
type ScanResult struct {
	CIDR       string      `json:"cidr"`
	HostCount  int         `json:"host_count"`
	Found      []FoundHost `json:"found"`
	Hints      []string    `json:"hints,omitempty"`
	DurationMs int64       `json:"duration_ms"`
}

// Scanner executes subnet discovery probes and optional device auto-registration.
type Scanner struct {
	snmpClient *snmp.Client
	repo       *postgres.Repo
	logger     *zap.Logger
}

// NewScanner creates a discovery scanner with SNMP client and device repository.
func NewScanner(snmpClient *snmp.Client, repo *postgres.Repo, logger *zap.Logger) *Scanner {
	return &Scanner{
		snmpClient: snmpClient,
		repo:       repo,
		logger:     logger,
	}
}

// ScanCIDR keeps legacy behavior (v2c/public + TCP prefilter + auto-add).
func (s *Scanner) ScanCIDR(ctx context.Context, cidr string) ([]string, error) {
	res, err := s.ScanNetwork(ctx, ScanParams{
		CIDR:         cidr,
		Community:    "public",
		SNMPVersion:  "v2c",
		AutoAdd:      true,
		TCPPrefilter: true,
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(res.Found))
	for _, h := range res.Found {
		out = append(out, h.IP)
	}
	return out, nil
}

// ScanNetwork iterates CIDR hosts and probes sysDescr via SNMP GET.
func (s *Scanner) ScanNetwork(ctx context.Context, p ScanParams) (*ScanResult, error) {
	start := time.Now()
	_, ipNet, err := net.ParseCIDR(strings.TrimSpace(p.CIDR))
	if err != nil {
		return nil, err
	}

	ver := domain.NormalizeSNMPVersionOrDefault(p.SNMPVersion)
	if ver == "v3" {
		if strings.TrimSpace(p.AuthProto) == "" || p.AuthPass == "" {
			return nil, &ScanError{Msg: "for snmp_version=v3 require auth_proto and auth_pass"}
		}
		if (strings.TrimSpace(p.PrivProto) == "") != (p.PrivPass == "") {
			return nil, &ScanError{Msg: "for snmp_version=v3 require both priv_proto and priv_pass or neither"}
		}
	}

	comm := strings.TrimSpace(p.Community)
	if comm == "" {
		comm = "public"
	}

	maxHosts := p.MaxHosts
	if maxHosts <= 0 {
		maxHosts = defaultMaxHosts
	}
	workers := p.Concurrency
	if workers <= 0 {
		workers = defaultWorkers
	}

	ips := generateIPs(ipNet)
	if len(ips) > maxHosts {
		return nil, &ScanError{Msg: "too many addresses in CIDR (limit " + strconv.Itoa(maxHosts) + "), use a smaller prefix"}
	}

	s.logger.Info("SNMP network scan started",
		zap.String("cidr", p.CIDR),
		zap.Int("hosts", len(ips)),
		zap.String("snmp_version", ver),
		zap.Bool("tcp_prefilter", p.TCPPrefilter),
		zap.Bool("auto_add", p.AutoAdd),
	)

	found := make([]FoundHost, 0)
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	jobs := make(chan string)

	base := domain.Device{
		Community:   comm,
		SNMPVersion: ver,
		AuthProto:   p.AuthProto,
		AuthPass:    p.AuthPass,
		PrivProto:   p.PrivProto,
		PrivPass:    p.PrivPass,
	}

	workerFn := func() {
		defer wg.Done()
		for ipStr := range jobs {
			if ctx.Err() != nil {
				return
			}
			if p.TCPPrefilter && !tcpPing(ipStr) {
				continue
			}

			probe := base
			probe.IP = ipStr
			result, err := s.snmpClient.GetDevice(&probe, []string{sysDescrOID})
			if err != nil {
				s.logger.Debug("scan probe failed",
					zap.String("ip", ipStr),
					zap.String("kind", string(snmp.GetErrorKind(err))),
					zap.Error(err))
				continue
			}
			if len(result) == 0 {
				continue
			}
			// gosnmp may return OID keys with/without leading dot.
			desc := strings.TrimSpace(mibresolver.PickSNMPValue(result, sysDescrOID))
			if desc == "" {
				continue
			}

			host := FoundHost{IP: ipStr, SysDescr: desc, Added: false}
			if p.AutoAdd {
				name := deviceNameFromDescr(ipStr, desc)
				d := probe
				d.Name = name
				if err := s.repo.CreateDevice(ctx, &d); err != nil {
					s.logger.Debug("scan skip insert", zap.String("ip", ipStr), zap.Error(err))
				} else {
					host.Added = true
					s.logger.Info("SNMP device added from scan", zap.String("ip", ipStr))
				}
			}

			mu.Lock()
			found = append(found, host)
			mu.Unlock()
		}
	}

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go workerFn()
	}
	for _, ip := range ips {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		case jobs <- ip.String():
		}
	}
	close(jobs)

	wg.Wait()

	s.logger.Info("SNMP network scan finished",
		zap.Int("found", len(found)),
		zap.Int64("ms", time.Since(start).Milliseconds()),
	)

	res := &ScanResult{
		CIDR:       p.CIDR,
		HostCount:  len(ips),
		Found:      found,
		DurationMs: time.Since(start).Milliseconds(),
	}
	if len(found) == 0 && len(ips) > 0 {
		res.Hints = emptyScanHints(p)
	}
	return res, nil
}

// emptyScanHints returns operator hints when a scan finds no reachable agents.
func emptyScanHints(p ScanParams) []string {
	ver := domain.NormalizeSNMPVersionOrDefault(p.SNMPVersion)
	h := []string{
		"Проверьте community и snmp_version: по умолчанию v2c и community \"public\"; при другом community или только SNMPv3 укажите их в теле запроса.",
		"Если NMS (api) запущен в Docker, контейнер должен иметь сетевой доступ к этой подсети (часто нужен host network, macvlan или маршрут; иначе UDP/SNMP до коммутаторов не доходит).",
		"Убедитесь, что на коммутаторах и на пути к ним не блокируется UDP/161 к SNMP-агенту от IP хоста NMS.",
	}
	if p.TCPPrefilter {
		h = append(h, "tcp_prefilter включён: устройства без открытых TCP-портов (80/443/22/21/161) не проверяются по SNMP — отключите tcp_prefilter для «чистого» SNMP.")
	}
	if ver == "v2c" {
		h = append(h, "Если коммутаторы только SNMPv3, запросите snmp_version \"v3\" и заполните auth_proto/auth_pass (и priv при необходимости).")
	}
	return h
}

// ScanError represents scan input validation errors (HTTP 4xx at handlers).
type ScanError struct {
	Msg string
}

// Error implements error for scan parameter validation failures.
func (e *ScanError) Error() string { return e.Msg }

// deviceNameFromDescr builds a sanitized default device name from sysDescr.
func deviceNameFromDescr(ip, descr string) string {
	const maxLen = 120
	var b strings.Builder
	for _, r := range descr {
		if len(b.String()) >= maxLen {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' || r == ' ' {
			b.WriteRune(r)
		} else if r == '\n' || r == '\r' || r == '\t' {
			b.WriteRune(' ')
		}
	}
	name := strings.TrimSpace(b.String())
	if name == "" {
		return "SNMP-" + ip
	}
	return name
}

// tcpPing checks whether host has at least one common management TCP port open.
func tcpPing(host string) bool {
	ports := []string{"80", "443", "22", "21", "161"}
	for _, port := range ports {
		conn, err := net.DialTimeout("tcp", host+":"+port, 1*time.Second)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

// generateIPs returns probe candidates, skipping network/broadcast for common IPv4 ranges.
func generateIPs(ipNet *net.IPNet) []net.IP {
	var ips []net.IP
	for ip := ipNet.IP.Mask(ipNet.Mask); ipNet.Contains(ip); incIP(ip) {
		ips = append(ips, append(net.IP(nil), ip...))
	}
	if len(ips) == 0 {
		return nil
	}
	if len(ips) <= 2 {
		return ips
	}
	return ips[1 : len(ips)-1]
}

// incIP increments an IP address in-place by one.
func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
