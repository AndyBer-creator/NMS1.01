package lldp

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"NMS1/internal/domain"
	"NMS1/internal/infrastructure/postgres"

	"go.uber.org/zap"
)

const (
	lldpLocPortDescBase = "1.0.8802.1.1.2.1.3.7.1.4"
	lldpLocPortIdBase   = "1.0.8802.1.1.2.1.3.7.1.3"

	lldpRemSysNameBase  = "1.0.8802.1.1.2.1.4.1.1.9"
	lldpRemSysDescBase  = "1.0.8802.1.1.2.1.4.1.1.10"
	lldpRemPortIdBase   = "1.0.8802.1.1.2.1.4.1.1.7"
	lldpRemPortDescBase = "1.0.8802.1.1.2.1.4.1.1.8"

	defaultLLDPDeviceWalkTimeout = 15 * time.Second
	defaultLLDPMaxRemoteEntries  = 4096
)

type remoteEntry struct {
	LocalPortNum int
	RemIndex     int

	SysName string
	SysDesc string

	PortID   string
	PortDesc string
}

// ScanParams keeps LLDP scan options placeholder for future extensions.
type ScanParams struct {
	// Reserved for future LLDP scan options.
}

// ScanSummary contains counters collected during one LLDP topology snapshot.
type ScanSummary struct {
	ScanID         int64
	DevicesScanned int
	LinksFound     int
	LinksInserted  int
}

type lldpRepo interface {
	ListDevices(ctx context.Context) ([]*domain.Device, error)
	CreateLldpScan(ctx context.Context) (int64, error)
	DeleteLldpScan(ctx context.Context, scanID int64) error
	InsertLldpLink(ctx context.Context, scanID int64, link postgres.LldpLink) (int64, error)
}

type lldpWalker interface {
	WalkDevice(device *domain.Device, baseOID string) (map[string]string, error)
}

func lldpDeviceWalkTimeout() time.Duration {
	v := strings.TrimSpace(os.Getenv("NMS_LLDP_DEVICE_WALK_TIMEOUT"))
	if v == "" {
		return defaultLLDPDeviceWalkTimeout
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return defaultLLDPDeviceWalkTimeout
	}
	return d
}

func lldpMaxRemoteEntries() int {
	v := strings.TrimSpace(os.Getenv("NMS_LLDP_MAX_REMOTE_ENTRIES"))
	if v == "" {
		return defaultLLDPMaxRemoteEntries
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultLLDPMaxRemoteEntries
	}
	return n
}

func walkWithTimeout(ctx context.Context, timeout time.Duration, walkFn func() (map[string]string, error)) (map[string]string, error) {
	if timeout <= 0 {
		timeout = defaultLLDPDeviceWalkTimeout
	}
	walkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		values map[string]string
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := walkFn()
		ch <- result{values: v, err: err}
	}()
	select {
	case <-walkCtx.Done():
		return nil, walkCtx.Err()
	case out := <-ch:
		return out.values, out.err
	}
}

// normalizeKey trims and lowercases inventory keys for map matching.
func normalizeKey(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// normalizeOID returns OID without surrounding spaces and leading dot.
func normalizeOID(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), ".")
}

// parseSingleIndexFromWalk parses "<base>.<index>" OID walk rows.
func parseSingleIndexFromWalk(fullOID, baseOID string) (int, bool) {
	fullOID = normalizeOID(fullOID)
	baseOID = normalizeOID(baseOID)
	suffix := strings.TrimPrefix(fullOID, baseOID+".")
	if suffix == fullOID {
		return 0, false
	}
	parts := strings.Split(suffix, ".")
	if len(parts) < 1 {
		return 0, false
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	return n, true
}

// LLDP remote entries are indexed as (timeMark, localPortNum, remIndex).
func parseRemoteIndexes(fullOID, baseOID string) (localPortNum int, remIndex int, ok bool) {
	fullOID = normalizeOID(fullOID)
	baseOID = normalizeOID(baseOID)
	suffix := strings.TrimPrefix(fullOID, baseOID+".")
	if suffix == fullOID {
		return 0, 0, false
	}
	parts := strings.Split(suffix, ".")
	if len(parts) < 3 {
		return 0, 0, false
	}
	// parts[0] = timeMark
	localPortNum, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	remIndex, err = strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, false
	}
	return localPortNum, remIndex, true
}

// ScanAllDevicesLLDP builds one LLDP topology snapshot and persists links.
func ScanAllDevicesLLDP(ctx context.Context, repo lldpRepo, client lldpWalker, logger *zap.Logger, _ ScanParams) (*ScanSummary, error) {
	// Device status/version are mostly relevant for credentials selection here.
	devices, err := repo.ListDevices(ctx)
	if err != nil {
		return nil, err
	}

	// name -> ip map improves matching remote sysName to known inventory.
	nameToIP := make(map[string]string, len(devices))
	for _, d := range devices {
		if d == nil {
			continue
		}
		if strings.TrimSpace(d.Name) == "" {
			continue
		}
		nameToIP[normalizeKey(d.Name)] = d.IP
	}

	scanID, err := repo.CreateLldpScan(ctx)
	if err != nil {
		return nil, err
	}

	logger.Info("LLDP scan started", zap.Int("devices", len(devices)), zap.Int64("scan_id", scanID))

	summary := &ScanSummary{ScanID: scanID}
	deviceWalkTimeout := lldpDeviceWalkTimeout()
	maxRemoteEntries := lldpMaxRemoteEntries()
	cleanupAbortedScan := func(reason error) {
		if reason == nil || scanID <= 0 {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if derr := repo.DeleteLldpScan(cleanupCtx, scanID); derr != nil {
			logger.Warn("LLDP aborted scan cleanup failed", zap.Int64("scan_id", scanID), zap.Error(derr))
			return
		}
		logger.Warn("LLDP aborted scan cleaned up", zap.Int64("scan_id", scanID), zap.Error(reason))
	}

	// We walk each LLDP table; this is periodic and acceptable every few minutes.
	for _, device := range devices {
		select {
		case <-ctx.Done():
			cleanupAbortedScan(ctx.Err())
			return summary, ctx.Err()
		default:
		}
		if device == nil {
			continue
		}

		// Prepare credentials using device copy.
		d := device

		// local ports mapping
		locPortDescMap := make(map[int]string)
		locPortIdMap := make(map[int]string)

		locPortDesc, err := walkWithTimeout(ctx, deviceWalkTimeout, func() (map[string]string, error) {
			return client.WalkDevice(d, lldpLocPortDescBase)
		})
		if err != nil {
			logger.Warn("LLDP: skip device due to incomplete walk set", zap.String("ip", d.IP), zap.String("table", "lldpLocPortDesc"), zap.Error(err))
			continue
		}
		for fullOID, val := range locPortDesc {
			if portNum, ok := parseSingleIndexFromWalk(fullOID, lldpLocPortDescBase); ok {
				locPortDescMap[portNum] = val
			}
		}

		locPortId, err := walkWithTimeout(ctx, deviceWalkTimeout, func() (map[string]string, error) {
			return client.WalkDevice(d, lldpLocPortIdBase)
		})
		if err != nil {
			logger.Warn("LLDP: skip device due to incomplete walk set", zap.String("ip", d.IP), zap.String("table", "lldpLocPortId"), zap.Error(err))
			continue
		}
		for fullOID, val := range locPortId {
			if portNum, ok := parseSingleIndexFromWalk(fullOID, lldpLocPortIdBase); ok {
				locPortIdMap[portNum] = val
			}
		}

		remoteEntries := make(map[string]*remoteEntry) // key = localPortNum/remIndex

		// remote sysName
		walkSysName, err := walkWithTimeout(ctx, deviceWalkTimeout, func() (map[string]string, error) {
			return client.WalkDevice(d, lldpRemSysNameBase)
		})
		if err != nil {
			logger.Warn("LLDP: skip device due to incomplete walk set", zap.String("ip", d.IP), zap.String("table", "lldpRemSysName"), zap.Error(err))
			continue
		}
		for fullOID, val := range walkSysName {
			localPortNum, remIndex, ok := parseRemoteIndexes(fullOID, lldpRemSysNameBase)
			if !ok {
				continue
			}
			key := strconv.Itoa(localPortNum) + "/" + strconv.Itoa(remIndex)
			entry := remoteEntries[key]
			if entry == nil {
				if len(remoteEntries) >= maxRemoteEntries {
					logger.Warn("LLDP: remote entry cap reached, skipping remaining entries",
						zap.String("ip", d.IP),
						zap.Int("max_remote_entries", maxRemoteEntries))
					break
				}
				entry = &remoteEntry{LocalPortNum: localPortNum, RemIndex: remIndex}
				remoteEntries[key] = entry
			}
			entry.SysName = strings.TrimSpace(val)
		}

		// remote sysDesc
		walkSysDesc, err := walkWithTimeout(ctx, deviceWalkTimeout, func() (map[string]string, error) {
			return client.WalkDevice(d, lldpRemSysDescBase)
		})
		if err != nil {
			logger.Warn("LLDP: skip device due to incomplete walk set", zap.String("ip", d.IP), zap.String("table", "lldpRemSysDesc"), zap.Error(err))
			continue
		}
		for fullOID, val := range walkSysDesc {
			localPortNum, remIndex, ok := parseRemoteIndexes(fullOID, lldpRemSysDescBase)
			if !ok {
				continue
			}
			key := strconv.Itoa(localPortNum) + "/" + strconv.Itoa(remIndex)
			entry := remoteEntries[key]
			if entry == nil {
				if len(remoteEntries) >= maxRemoteEntries {
					logger.Warn("LLDP: remote entry cap reached, skipping remaining entries",
						zap.String("ip", d.IP),
						zap.Int("max_remote_entries", maxRemoteEntries))
					break
				}
				entry = &remoteEntry{LocalPortNum: localPortNum, RemIndex: remIndex}
				remoteEntries[key] = entry
			}
			entry.SysDesc = strings.TrimSpace(val)
		}

		// remote port ID/desc
		walkPortId, err := walkWithTimeout(ctx, deviceWalkTimeout, func() (map[string]string, error) {
			return client.WalkDevice(d, lldpRemPortIdBase)
		})
		if err != nil {
			logger.Warn("LLDP: skip device due to incomplete walk set", zap.String("ip", d.IP), zap.String("table", "lldpRemPortId"), zap.Error(err))
			continue
		}
		for fullOID, val := range walkPortId {
			localPortNum, remIndex, ok := parseRemoteIndexes(fullOID, lldpRemPortIdBase)
			if !ok {
				continue
			}
			key := strconv.Itoa(localPortNum) + "/" + strconv.Itoa(remIndex)
			entry := remoteEntries[key]
			if entry == nil {
				if len(remoteEntries) >= maxRemoteEntries {
					logger.Warn("LLDP: remote entry cap reached, skipping remaining entries",
						zap.String("ip", d.IP),
						zap.Int("max_remote_entries", maxRemoteEntries))
					break
				}
				entry = &remoteEntry{LocalPortNum: localPortNum, RemIndex: remIndex}
				remoteEntries[key] = entry
			}
			entry.PortID = strings.TrimSpace(val)
		}

		walkPortDesc, err := walkWithTimeout(ctx, deviceWalkTimeout, func() (map[string]string, error) {
			return client.WalkDevice(d, lldpRemPortDescBase)
		})
		if err != nil {
			logger.Warn("LLDP: skip device due to incomplete walk set", zap.String("ip", d.IP), zap.String("table", "lldpRemPortDesc"), zap.Error(err))
			continue
		}
		for fullOID, val := range walkPortDesc {
			localPortNum, remIndex, ok := parseRemoteIndexes(fullOID, lldpRemPortDescBase)
			if !ok {
				continue
			}
			key := strconv.Itoa(localPortNum) + "/" + strconv.Itoa(remIndex)
			entry := remoteEntries[key]
			if entry == nil {
				if len(remoteEntries) >= maxRemoteEntries {
					logger.Warn("LLDP: remote entry cap reached, skipping remaining entries",
						zap.String("ip", d.IP),
						zap.Int("max_remote_entries", maxRemoteEntries))
					break
				}
				entry = &remoteEntry{LocalPortNum: localPortNum, RemIndex: remIndex}
				remoteEntries[key] = entry
			}
			entry.PortDesc = strings.TrimSpace(val)
		}

		// Persist links.
		linksFound := 0
		linksInserted := 0
		for _, entry := range remoteEntries {
			if entry == nil {
				continue
			}
			if entry.SysName == "" && entry.SysDesc == "" {
				continue
			}
			linksFound++

			localPortDesc := locPortDescMap[entry.LocalPortNum]
			if strings.TrimSpace(localPortDesc) == "" {
				localPortDesc = locPortIdMap[entry.LocalPortNum]
			}

			remoteIPPtr := (*string)(nil)
			if entry.SysName != "" {
				if remoteIP, ok := nameToIP[normalizeKey(entry.SysName)]; ok {
					remoteIPCopy := remoteIP
					remoteIPPtr = &remoteIPCopy
				}
			}

			link := postgres.LldpLink{
				LocalDeviceIP: d.IP,
				LocalPortNum:  entry.LocalPortNum,
				LocalPortDesc: localPortDesc,

				RemoteDeviceIP: remoteIPPtr,
				RemoteSysName:  entry.SysName,
				RemoteSysDesc:  entry.SysDesc,

				RemotePortID:   entry.PortID,
				RemotePortDesc: entry.PortDesc,
			}

			inserted, err := repo.InsertLldpLink(ctx, scanID, link)
			if err != nil {
				logger.Warn("LLDP: insert link failed", zap.String("local_ip", d.IP), zap.Error(err))
				continue
			}
			linksInserted += int(inserted)
		}

		summary.DevicesScanned++
		summary.LinksFound += linksFound
		summary.LinksInserted += linksInserted

		logger.Info("LLDP device scanned",
			zap.String("ip", d.IP),
			zap.Int("links_found", linksFound),
			zap.Int("links_inserted", linksInserted),
			zap.Int("remote_entries", len(remoteEntries)),
		)
	}

	logger.Info("LLDP scan finished", zap.Int64("scan_id", scanID))
	return summary, nil
}
