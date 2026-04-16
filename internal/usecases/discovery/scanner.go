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

// ScanParams задаёт параметры поиска SNMP-агентов в подсети.
type ScanParams struct {
	CIDR         string
	Community    string // v2c: community; v3: имя пользователя (как в БД)
	SNMPVersion  string
	AuthProto    string
	AuthPass     string
	PrivProto    string
	PrivPass     string
	AutoAdd      bool // добавлять найденные IP в devices
	TCPPrefilter bool // сначала проверять открытые TCP-порты (часто пропускает «чистый» SNMP)
	Concurrency  int  // 0 → defaultWorkers
	MaxHosts     int  // 0 → defaultMaxHosts, верхняя граница числа проверяемых адресов
}

// FoundHost — один ответивший по SNMP хост.
type FoundHost struct {
	IP       string `json:"ip"`
	SysDescr string `json:"sys_descr,omitempty"`
	Added    bool   `json:"added"` // успешно вставлен в БД (AutoAdd и не было дубликата)
}

// ScanResult — итог сканирования.
type ScanResult struct {
	CIDR       string      `json:"cidr"`
	HostCount  int         `json:"host_count"`
	Found      []FoundHost `json:"found"`
	Hints      []string    `json:"hints,omitempty"`
	DurationMs int64       `json:"duration_ms"`
}

type Scanner struct {
	snmpClient *snmp.Client
	repo       *postgres.Repo
	logger     *zap.Logger
}

func NewScanner(snmpClient *snmp.Client, repo *postgres.Repo, logger *zap.Logger) *Scanner {
	return &Scanner{
		snmpClient: snmpClient,
		repo:       repo,
		logger:     logger,
	}
}

// ScanCIDR оставлен для обратной совместимости: v2c/public, TCP-префильтр, авто-добавление в БД.
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

// ScanNetwork перебирает адреса в CIDR и проверяет SNMP Get sysDescr.
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
	sem := make(chan struct{}, workers)

	base := domain.Device{
		Community:   comm,
		SNMPVersion: ver,
		AuthProto:   p.AuthProto,
		AuthPass:    p.AuthPass,
		PrivProto:   p.PrivProto,
		PrivPass:    p.PrivPass,
	}

	for _, ip := range ips {
		ipStr := ip.String()
		wg.Add(1)
		go func(ipStr string) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			if p.TCPPrefilter && !tcpPing(ipStr) {
				return
			}

			probe := base
			probe.IP = ipStr
			result, err := s.snmpClient.GetDevice(&probe, []string{sysDescrOID})
			if err != nil {
				s.logger.Debug("scan probe failed",
					zap.String("ip", ipStr),
					zap.String("kind", string(snmp.GetErrorKind(err))),
					zap.Error(err))
				return
			}
			if len(result) == 0 {
				return
			}
			// gosnmp может вернуть OID-ключ с ведущей точкой. Берем значение устойчиво.
			desc := strings.TrimSpace(mibresolver.PickSNMPValue(result, sysDescrOID))
			if desc == "" {
				return
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
		}(ipStr)
	}

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

// ScanError — ошибка валидации параметров сканирования (4xx).
type ScanError struct {
	Msg string
}

func (e *ScanError) Error() string { return e.Msg }

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

// generateIPs возвращает адреса для опроса: для типичной IPv4-подсети без network и broadcast.
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

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
