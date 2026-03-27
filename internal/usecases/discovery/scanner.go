package discovery

import (
	"context"
	"net"
	"sync"
	"time"

	"NMS1/internal/domain"
	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"

	"go.uber.org/zap"
)

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

func (s *Scanner) ScanCIDR(ctx context.Context, cidr string) ([]string, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	s.logger.Info("ICMP+SNMP scan started", zap.String("cidr", cidr))

	var (
		liveHosts []string
		mu        sync.Mutex
		wg        sync.WaitGroup
	)

	ips := generateIPs(ipNet)
	semaphore := make(chan struct{}, 100)

	for _, ip := range ips {
		wg.Add(1)
		go func(ipStr string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			select {
			case <-ctx.Done():
				return
			default:
			}

			if !tcpPing(ipStr) {
				return
			}

			tmpDevice := &domain.Device{
				IP:          ipStr,
				Community:   "public",
				SNMPVersion: "v2c",
			}
			result, err := s.snmpClient.GetDevice(tmpDevice, []string{"1.3.6.1.2.1.1.1.0"})
			if err == nil && len(result) > 0 {
				mu.Lock()
				liveHosts = append(liveHosts, ipStr)
				mu.Unlock()

				device := &domain.Device{
					IP:        ipStr,
					Name:      "SNMP-" + ipStr,
					Community: "public",
				}
				_ = s.repo.CreateDevice(device)
				s.logger.Info("SNMP device found", zap.String("ip", ipStr))
			}
		}(ip.String())
	}

	wg.Wait()
	s.logger.Info("Scan finished", zap.Int("live_hosts", len(liveHosts)))
	return liveHosts, nil
}

func tcpPing(host string) bool {
	ports := []string{"80", "443", "22", "21", "161"}
	for _, port := range ports {
		conn, err := net.DialTimeout("tcp", host+":"+port, 1*time.Second)
		if err == nil {
			conn.Close()
			return true
		}
	}
	return false
}

// func icmpPing(host string) bool {
// 	c, err := icmp.ListenPacket(context.Background(), icmp.IPv4EchoRequest, "")
// 	if err != nil {
// 		return false
// 	}
// 	defer c.Close()

// 	msg := icmp.Echo{
// 		ID:   os.Getpid() & 0xffff,
// 		Seq:  1,
// 		Body: []byte("NMS1"),
// 	}

// 	b, _ := msg.Marshal(nil) // nil = IPv4
// 	addr := net.ParseIP(host)

// 	_, err = c.WriteTo(b, addr)
// 	if err != nil {
// 		return false
// 	}

// 	c.SetReadDeadline(time.Now().Add(1 * time.Second))
// 	_, _, err = c.ReadFrom(1024)
// 	return err == nil
// }

func generateIPs(ipNet *net.IPNet) []net.IP {
	var ips []net.IP
	for ip := ipNet.IP.Mask(ipNet.Mask); ipNet.Contains(ip); incIP(ip) {
		ips = append(ips, append(net.IP(nil), ip...))
	}
	return ips[:len(ips)-1]
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
