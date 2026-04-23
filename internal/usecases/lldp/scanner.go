package lldp

import (
	"context"
	"strconv"
	"strings"

	"NMS1/internal/infrastructure/postgres"
	"NMS1/internal/infrastructure/snmp"

	"go.uber.org/zap"
)

const (
	lldpLocPortDescBase = "1.0.8802.1.1.2.1.3.7.1.4"
	lldpLocPortIdBase   = "1.0.8802.1.1.2.1.3.7.1.3"

	lldpRemSysNameBase  = "1.0.8802.1.1.2.1.4.1.1.9"
	lldpRemSysDescBase  = "1.0.8802.1.1.2.1.4.1.1.10"
	lldpRemPortIdBase   = "1.0.8802.1.1.2.1.4.1.1.7"
	lldpRemPortDescBase = "1.0.8802.1.1.2.1.4.1.1.8"
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
func ScanAllDevicesLLDP(ctx context.Context, repo *postgres.Repo, client *snmp.Client, logger *zap.Logger, _ ScanParams) (*ScanSummary, error) {
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

	// We walk each LLDP table; this is periodic and acceptable every few minutes.
	for _, device := range devices {
		select {
		case <-ctx.Done():
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

		locPortDesc, err := client.WalkDevice(d, lldpLocPortDescBase)
		if err == nil {
			for fullOID, val := range locPortDesc {
				if portNum, ok := parseSingleIndexFromWalk(fullOID, lldpLocPortDescBase); ok {
					locPortDescMap[portNum] = val
				}
			}
		} else {
			logger.Warn("LLDP: failed walk lldpLocPortDesc", zap.String("ip", d.IP), zap.Error(err))
		}

		locPortId, err := client.WalkDevice(d, lldpLocPortIdBase)
		if err == nil {
			for fullOID, val := range locPortId {
				if portNum, ok := parseSingleIndexFromWalk(fullOID, lldpLocPortIdBase); ok {
					locPortIdMap[portNum] = val
				}
			}
		} else {
			logger.Warn("LLDP: failed walk lldpLocPortId", zap.String("ip", d.IP), zap.Error(err))
		}

		remoteEntries := make(map[string]*remoteEntry) // key = localPortNum/remIndex

		// remote sysName
		walkSysName, err := client.WalkDevice(d, lldpRemSysNameBase)
		if err != nil {
			logger.Warn("LLDP: failed walk lldpRemSysName", zap.String("ip", d.IP), zap.Error(err))
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
				entry = &remoteEntry{LocalPortNum: localPortNum, RemIndex: remIndex}
				remoteEntries[key] = entry
			}
			entry.SysName = strings.TrimSpace(val)
		}

		// remote sysDesc
		walkSysDesc, err := client.WalkDevice(d, lldpRemSysDescBase)
		if err == nil {
			for fullOID, val := range walkSysDesc {
				localPortNum, remIndex, ok := parseRemoteIndexes(fullOID, lldpRemSysDescBase)
				if !ok {
					continue
				}
				key := strconv.Itoa(localPortNum) + "/" + strconv.Itoa(remIndex)
				entry := remoteEntries[key]
				if entry == nil {
					entry = &remoteEntry{LocalPortNum: localPortNum, RemIndex: remIndex}
					remoteEntries[key] = entry
				}
				entry.SysDesc = strings.TrimSpace(val)
			}
		}

		// remote port ID/desc
		walkPortId, err := client.WalkDevice(d, lldpRemPortIdBase)
		if err == nil {
			for fullOID, val := range walkPortId {
				localPortNum, remIndex, ok := parseRemoteIndexes(fullOID, lldpRemPortIdBase)
				if !ok {
					continue
				}
				key := strconv.Itoa(localPortNum) + "/" + strconv.Itoa(remIndex)
				entry := remoteEntries[key]
				if entry == nil {
					entry = &remoteEntry{LocalPortNum: localPortNum, RemIndex: remIndex}
					remoteEntries[key] = entry
				}
				entry.PortID = strings.TrimSpace(val)
			}
		}

		walkPortDesc, err := client.WalkDevice(d, lldpRemPortDescBase)
		if err == nil {
			for fullOID, val := range walkPortDesc {
				localPortNum, remIndex, ok := parseRemoteIndexes(fullOID, lldpRemPortDescBase)
				if !ok {
					continue
				}
				key := strconv.Itoa(localPortNum) + "/" + strconv.Itoa(remIndex)
				entry := remoteEntries[key]
				if entry == nil {
					entry = &remoteEntry{LocalPortNum: localPortNum, RemIndex: remIndex}
					remoteEntries[key] = entry
				}
				entry.PortDesc = strings.TrimSpace(val)
			}
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
