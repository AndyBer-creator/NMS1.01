package snmp

import (
	"fmt"
	"NMS1/internal/domain"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
	"go.uber.org/zap"
)

type Client struct {
	Port    int
	Timeout time.Duration
	Retries int
	logger  *zap.Logger
}

func New(port int, timeout time.Duration, retries int) *Client {
	return &Client{
		Port:    port,
		Timeout: timeout,
		Retries: retries,
		logger:  zap.L(),
	}
}

func (c *Client) Get(ip string, community string, oids []string) (map[string]string, error) {
	return c.getV2c(ip, community, oids)
}

func (c *Client) GetDevice(device *domain.Device, oids []string) (map[string]string, error) {
	switch strings.ToLower(device.SNMPVersion) {
	case "v3":
		return c.getV3(device, oids)
	case "v1":
		// Для управления/опроса v1 нужна другая реализация (Community+Version1).
		// Пока используем v2c-совместимый режим.
		return c.getV2c(device.IP, device.Community, oids)
	case "", "v2c":
		fallthrough
	default:
		return c.getV2c(device.IP, device.Community, oids)
	}
}

// WalkDevice делает SNMP WALK/BULKWALK по базовому OID и возвращает map[fullOID]value.
// Нужен для LLDP (lldpLocPortTable/lldpRemTable).
func (c *Client) WalkDevice(device *domain.Device, baseOID string) (map[string]string, error) {
	switch strings.ToLower(device.SNMPVersion) {
	case "v3":
		return c.walkV3(device, baseOID)
	case "v1":
		// LLDP обычно на v2c/v3; v1 пока не поддерживаем в WALK (fallback на v2c).
		return c.walkV2c(device.IP, device.Community, baseOID)
	case "", "v2c":
		fallthrough
	default:
		return c.walkV2c(device.IP, device.Community, baseOID)
	}
}

func (c *Client) SetDevice(device *domain.Device, oid string, pduType gosnmp.Asn1BER, value interface{}) error {
	switch strings.ToLower(device.SNMPVersion) {
	case "v3":
		return c.setV3(device, oid, pduType, value)
	case "v1":
		// Для управления v1 нужна отдельная реализация. Пока пробуем отправить как v2c.
		return c.setV2c(device.IP, device.Community, oid, pduType, value)
	case "", "v2c":
		fallthrough
	default:
		return c.setV2c(device.IP, device.Community, oid, pduType, value)
	}
}

func (c *Client) getV2c(ip string, community string, oids []string) (map[string]string, error) {
	conn := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      uint16(c.Port),
		Community: community, // ✅ Простая строка вместо device
		Timeout:   c.Timeout,
		Version:   gosnmp.Version2c,
		Retries:   c.Retries,
	}

	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Conn.Close()

	pdu, err := conn.Get(oids)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}

	result := make(map[string]string)
	for _, v := range pdu.Variables {
		result[v.Name] = fmt.Sprintf("%v", v.Value)
	}
	return result, nil
}

func (c *Client) versionFromString(version string) gosnmp.SnmpVersion {
	switch version {
	case "v1":
		return gosnmp.Version1
	case "v3":
		return gosnmp.Version3
	default:
		return gosnmp.Version2c
	}
}

func (c *Client) getV3(device *domain.Device, oids []string) (map[string]string, error) {
	conn, err := c.newV3Conn(device)
	if err != nil {
		return nil, err
	}
	defer conn.Conn.Close()

	pdu, err := conn.Get(oids)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}

	result := make(map[string]string)
	for _, v := range pdu.Variables {
		result[v.Name] = fmt.Sprintf("%v", v.Value)
	}
	return result, nil
}

func (c *Client) walkV2c(ip string, community string, baseOID string) (map[string]string, error) {
	conn := &gosnmp.GoSNMP{
		Target:         ip,
		Port:           uint16(c.Port),
		Community:      community,
		Timeout:        c.Timeout,
		Version:        gosnmp.Version2c,
		Retries:        c.Retries,
		MaxRepetitions: 25,
	}

	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Conn.Close()

	pdus, err := conn.BulkWalkAll(baseOID)
	if err != nil {
		// fallback на обычный Walk, если Bulk не поддерживается/не сработал
		pdus2, err2 := conn.WalkAll(baseOID)
		if err2 != nil {
			return nil, fmt.Errorf("bulkwalk: %w (fallback walk: %v)", err, err2)
		}
		pdus = pdus2
	}

	result := make(map[string]string)
	for _, v := range pdus {
		result[v.Name] = fmt.Sprintf("%v", v.Value)
	}
	return result, nil
}

func (c *Client) walkV3(device *domain.Device, baseOID string) (map[string]string, error) {
	conn, err := c.newV3Conn(device)
	if err != nil {
		return nil, err
	}
	defer conn.Conn.Close()

	conn.MaxRepetitions = 25

	pdus, err := conn.BulkWalkAll(baseOID)
	if err != nil {
		// fallback на обычный Walk
		pdus2, err2 := conn.WalkAll(baseOID)
		if err2 != nil {
			return nil, fmt.Errorf("bulkwalk: %w (fallback walk: %v)", err, err2)
		}
		pdus = pdus2
	}

	result := make(map[string]string)
	for _, v := range pdus {
		result[v.Name] = fmt.Sprintf("%v", v.Value)
	}
	return result, nil
}

func (c *Client) setV2c(ip, community, oid string, pduType gosnmp.Asn1BER, value interface{}) error {
	conn := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      uint16(c.Port),
		Community: community,
		Timeout:   c.Timeout,
		Version:   gosnmp.Version2c,
		Retries:   c.Retries,
	}

	if err := conn.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Conn.Close()

	pdus := []gosnmp.SnmpPDU{
		{
			Name:  oid,
			Type:  pduType,
			Value: value,
		},
	}

	if _, err := conn.Set(pdus); err != nil {
		return fmt.Errorf("set: %w", err)
	}
	return nil
}

func (c *Client) setV3(device *domain.Device, oid string, pduType gosnmp.Asn1BER, value interface{}) error {
	conn, err := c.newV3Conn(device)
	if err != nil {
		return err
	}
	defer conn.Conn.Close()

	pdus := []gosnmp.SnmpPDU{
		{
			Name:  oid,
			Type:  pduType,
			Value: value,
		},
	}
	if _, err := conn.Set(pdus); err != nil {
		return fmt.Errorf("set: %w", err)
	}
	return nil
}

func (c *Client) newV3Conn(device *domain.Device) (*gosnmp.GoSNMP, error) {
	userName := strings.TrimSpace(device.Community) // для v3 используем community-колонку как username
	if userName == "" {
		return nil, fmt.Errorf("snmpv3: community/username must be set")
	}

	authEnabled := strings.TrimSpace(device.AuthProto) != "" && device.AuthPass != ""
	privEnabled := strings.TrimSpace(device.PrivProto) != "" && device.PrivPass != ""
	if privEnabled && !authEnabled {
		return nil, fmt.Errorf("snmpv3: priv requires auth (auth_proto/auth_pass must be set)")
	}

	var msgFlags gosnmp.SnmpV3MsgFlags
	switch {
	case privEnabled:
		msgFlags = gosnmp.AuthPriv
	case authEnabled:
		msgFlags = gosnmp.AuthNoPriv
	default:
		msgFlags = gosnmp.NoAuthNoPriv
	}

	authProtocol := c.authProtocol(strings.TrimSpace(device.AuthProto))
	privProtocol := c.privProtocol(strings.TrimSpace(device.PrivProto))

	if authEnabled && authProtocol <= gosnmp.NoAuth {
		return nil, fmt.Errorf("snmpv3: unsupported auth_proto=%q", device.AuthProto)
	}
	if privEnabled && privProtocol <= gosnmp.NoPriv {
		return nil, fmt.Errorf("snmpv3: unsupported priv_proto=%q", device.PrivProto)
	}

	conn := &gosnmp.GoSNMP{
		Target:             device.IP,
		Port:               uint16(c.Port),
		Version:            gosnmp.Version3,
		Timeout:            c.Timeout,
		Retries:            c.Retries,
		MsgFlags:           msgFlags,
		SecurityModel:      gosnmp.UserSecurityModel,
		SecurityParameters: &gosnmp.UsmSecurityParameters{
			UserName:                 userName,
			AuthenticationProtocol:   authProtocol,
			AuthenticationPassphrase: device.AuthPass,
			PrivacyProtocol:          privProtocol,
			PrivacyPassphrase:        device.PrivPass,
		},
	}

	// Connect() сам выполнит engine discovery, если AuthoritativeEngineID пустой.
	if err := conn.Connect(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return conn, nil
}

func (c *Client) authProtocol(proto string) gosnmp.SnmpV3AuthProtocol {
	proto = strings.ToUpper(proto)
	switch proto {
	case "MD5":
		return gosnmp.MD5
	case "SHA", "SHA1":
		return gosnmp.SHA
	case "SHA224":
		return gosnmp.SHA224
	case "SHA256":
		return gosnmp.SHA256
	case "SHA384":
		return gosnmp.SHA384
	case "SHA512":
		return gosnmp.SHA512
	default:
		return gosnmp.NoAuth
	}
}

func (c *Client) privProtocol(proto string) gosnmp.SnmpV3PrivProtocol {
	proto = strings.ToUpper(proto)
	switch proto {
	case "DES":
		return gosnmp.DES
	case "AES":
		// В gosnmp AES означает AES128 (по документации/типичным настройкам).
		return gosnmp.AES
	case "AES128":
		return gosnmp.AES
	case "AES192":
		return gosnmp.AES192
	case "AES256":
		return gosnmp.AES256
	case "AES192C":
		return gosnmp.AES192C
	case "AES256C":
		return gosnmp.AES256C
	default:
		return gosnmp.NoPriv
	}
}
