package snmp

import (
	"NMS1/internal/domain"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gosnmp/gosnmp"
	"go.uber.org/zap"
)

// Client wraps SNMP v2c/v3 operations with runtime-configurable transport settings.
type Client struct {
	Port    int
	Timeout time.Duration
	Retries int
	logger  *zap.Logger
	mu      sync.RWMutex
}

// RuntimeConfig represents current SNMP transport settings snapshot.
type RuntimeConfig struct {
	Port    int
	Timeout time.Duration
	Retries int
}

// ErrorKind classifies operational SNMP failures for observability and workflows.
type ErrorKind string

const (
	ErrorKindTimeout   ErrorKind = "timeout"
	ErrorKindAuth      ErrorKind = "auth"
	ErrorKindNoSuch    ErrorKind = "no_such_name"
	ErrorKindTransport ErrorKind = "transport"
)

// SNMPError wraps low-level errors with operation and normalized kind.
type SNMPError struct {
	Op   string
	Kind ErrorKind
	Err  error
}

// Error implements error for SNMPError.
func (e *SNMPError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s (%s): %v", e.Op, e.Kind, e.Err)
}

// Unwrap exposes the wrapped root cause error.
func (e *SNMPError) Unwrap() error { return e.Err }

// GetErrorKind extracts normalized error kind from any error value.
func GetErrorKind(err error) ErrorKind {
	var se *SNMPError
	if errors.As(err, &se) {
		return se.Kind
	}
	return ErrorKindTransport
}

func classifyErrorKind(err error) ErrorKind {
	s := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(s, "timeout"),
		strings.Contains(s, "i/o timeout"),
		strings.Contains(s, "request timeout"),
		strings.Contains(s, "no response"):
		return ErrorKindTimeout
	case strings.Contains(s, "authentication"),
		strings.Contains(s, "authorization"),
		strings.Contains(s, "unknown user"),
		strings.Contains(s, "usm"),
		strings.Contains(s, "decryption"),
		strings.Contains(s, "wrong digest"):
		return ErrorKindAuth
	case strings.Contains(s, "no such name"),
		strings.Contains(s, "no such object"),
		strings.Contains(s, "no such instance"):
		return ErrorKindNoSuch
	default:
		return ErrorKindTransport
	}
}

func wrapSNMPError(op string, err error) error {
	if err == nil {
		return nil
	}
	return &SNMPError{
		Op:   op,
		Kind: classifyErrorKind(err),
		Err:  err,
	}
}

// New creates SNMP client with initial transport settings.
func New(port int, timeout time.Duration, retries int) *Client {
	return &Client{
		Port:    port,
		Timeout: timeout,
		Retries: retries,
		logger:  zap.L(),
	}
}

// Config returns a thread-safe snapshot of runtime SNMP settings.
func (c *Client) Config() RuntimeConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return RuntimeConfig{
		Port:    c.Port,
		Timeout: c.Timeout,
		Retries: c.Retries,
	}
}

// ApplyRuntimeConfig updates timeout/retries used by future operations.
func (c *Client) ApplyRuntimeConfig(timeout time.Duration, retries int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Timeout = timeout
	c.Retries = retries
}

func snmpPortUint16(port int) uint16 {
	if port < 1 || port > 65535 {
		return 161
	}
	return uint16(port)
}

// pduValueString converts PDU values to API/UI friendly strings.
// OCTET STRING values often come as []byte and require text conversion.
func pduValueString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case []byte:
		return strings.TrimRight(string(x), "\x00")
	case string:
		return x
	default:
		return fmt.Sprintf("%v", x)
	}
}

// Get performs SNMP v2c GET for explicit host/community pair.
func (c *Client) Get(ip string, community string, oids []string) (map[string]string, error) {
	return c.getV2c(ip, community, oids)
}

// GetDevice resolves SNMP version/credentials from device and runs GET.
func (c *Client) GetDevice(device *domain.Device, oids []string) (map[string]string, error) {
	switch strings.ToLower(device.SNMPVersion) {
	case "v3":
		return c.getV3(device, oids)
	case "v1":
		// v1-specific behavior can be implemented later; use v2c-compatible path for now.
		return c.getV2c(device.IP, device.Community, oids)
	case "", "v2c":
		fallthrough
	default:
		return c.getV2c(device.IP, device.Community, oids)
	}
}

// WalkDevice performs SNMP WALK/BULKWALK and returns map[fullOID]value.
func (c *Client) WalkDevice(device *domain.Device, baseOID string) (map[string]string, error) {
	switch strings.ToLower(device.SNMPVersion) {
	case "v3":
		return c.walkV3(device, baseOID)
	case "v1":
		// LLDP is typically collected via v2c/v3; fallback to v2c path for v1.
		return c.walkV2c(device.IP, device.Community, baseOID)
	case "", "v2c":
		fallthrough
	default:
		return c.walkV2c(device.IP, device.Community, baseOID)
	}
}

// SetDevice resolves SNMP version/credentials from device and runs SET.
func (c *Client) SetDevice(device *domain.Device, oid string, pduType gosnmp.Asn1BER, value interface{}) error {
	switch strings.ToLower(device.SNMPVersion) {
	case "v3":
		return c.setV3(device, oid, pduType, value)
	case "v1":
		// v1 SET path can be specialized later; fallback to v2c for now.
		return c.setV2c(device.IP, device.Community, oid, pduType, value)
	case "", "v2c":
		fallthrough
	default:
		return c.setV2c(device.IP, device.Community, oid, pduType, value)
	}
}

func (c *Client) getV2c(ip string, community string, oids []string) (map[string]string, error) {
	cfg := c.Config()
	conn := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      snmpPortUint16(cfg.Port),
		Community: community,
		Timeout:   cfg.Timeout,
		Version:   gosnmp.Version2c,
		Retries:   cfg.Retries,
	}

	if err := conn.Connect(); err != nil {
		return nil, wrapSNMPError("connect", err)
	}
	defer func() { _ = conn.Conn.Close() }()

	pdu, err := conn.Get(oids)
	if err != nil {
		return nil, wrapSNMPError("get", err)
	}

	result := make(map[string]string)
	for _, v := range pdu.Variables {
		result[v.Name] = pduValueString(v.Value)
	}
	return result, nil
}

func (c *Client) getV3(device *domain.Device, oids []string) (map[string]string, error) {
	conn, err := c.newV3Conn(device)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Conn.Close() }()

	pdu, err := conn.Get(oids)
	if err != nil {
		return nil, wrapSNMPError("get", err)
	}

	result := make(map[string]string)
	for _, v := range pdu.Variables {
		result[v.Name] = pduValueString(v.Value)
	}
	return result, nil
}

func (c *Client) walkV2c(ip string, community string, baseOID string) (map[string]string, error) {
	cfg := c.Config()
	conn := &gosnmp.GoSNMP{
		Target:         ip,
		Port:           snmpPortUint16(cfg.Port),
		Community:      community,
		Timeout:        cfg.Timeout,
		Version:        gosnmp.Version2c,
		Retries:        cfg.Retries,
		MaxRepetitions: 25,
	}

	if err := conn.Connect(); err != nil {
		return nil, wrapSNMPError("connect", err)
	}
	defer func() { _ = conn.Conn.Close() }()

	pdus, err := conn.BulkWalkAll(baseOID)
	if err != nil {
		// Fallback to regular WALK if BULKWALK is unsupported/failed.
		pdus2, err2 := conn.WalkAll(baseOID)
		if err2 != nil {
			return nil, wrapSNMPError("bulkwalk", fmt.Errorf("%w (fallback walk: %v)", err, err2))
		}
		pdus = pdus2
	}

	result := make(map[string]string)
	for _, v := range pdus {
		result[v.Name] = pduValueString(v.Value)
	}
	return result, nil
}

func (c *Client) walkV3(device *domain.Device, baseOID string) (map[string]string, error) {
	conn, err := c.newV3Conn(device)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Conn.Close() }()

	conn.MaxRepetitions = 25

	pdus, err := conn.BulkWalkAll(baseOID)
	if err != nil {
		// Fallback to regular WALK.
		pdus2, err2 := conn.WalkAll(baseOID)
		if err2 != nil {
			return nil, wrapSNMPError("bulkwalk", fmt.Errorf("%w (fallback walk: %v)", err, err2))
		}
		pdus = pdus2
	}

	result := make(map[string]string)
	for _, v := range pdus {
		result[v.Name] = pduValueString(v.Value)
	}
	return result, nil
}

func (c *Client) setV2c(ip, community, oid string, pduType gosnmp.Asn1BER, value interface{}) error {
	cfg := c.Config()
	conn := &gosnmp.GoSNMP{
		Target:    ip,
		Port:      snmpPortUint16(cfg.Port),
		Community: community,
		Timeout:   cfg.Timeout,
		Version:   gosnmp.Version2c,
		Retries:   cfg.Retries,
	}

	if err := conn.Connect(); err != nil {
		return wrapSNMPError("connect", err)
	}
	defer func() { _ = conn.Conn.Close() }()

	pdus := []gosnmp.SnmpPDU{
		{
			Name:  oid,
			Type:  pduType,
			Value: value,
		},
	}

	if _, err := conn.Set(pdus); err != nil {
		return wrapSNMPError("set", err)
	}
	return nil
}

func (c *Client) setV3(device *domain.Device, oid string, pduType gosnmp.Asn1BER, value interface{}) error {
	conn, err := c.newV3Conn(device)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Conn.Close() }()

	pdus := []gosnmp.SnmpPDU{
		{
			Name:  oid,
			Type:  pduType,
			Value: value,
		},
	}
	if _, err := conn.Set(pdus); err != nil {
		return wrapSNMPError("set", err)
	}
	return nil
}

func (c *Client) newV3Conn(device *domain.Device) (*gosnmp.GoSNMP, error) {
	cfg := c.Config()
	userName := strings.TrimSpace(device.Community) // for v3 this field stores username
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
		Target:        device.IP,
		Port:          snmpPortUint16(cfg.Port),
		Version:       gosnmp.Version3,
		Timeout:       cfg.Timeout,
		Retries:       cfg.Retries,
		MsgFlags:      msgFlags,
		SecurityModel: gosnmp.UserSecurityModel,
		SecurityParameters: &gosnmp.UsmSecurityParameters{
			UserName:                 userName,
			AuthenticationProtocol:   authProtocol,
			AuthenticationPassphrase: device.AuthPass,
			PrivacyProtocol:          privProtocol,
			PrivacyPassphrase:        device.PrivPass,
		},
	}

	// Connect performs engine discovery when AuthoritativeEngineID is empty.
	if err := conn.Connect(); err != nil {
		return nil, wrapSNMPError("connect", err)
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
		// In gosnmp, AES maps to AES128.
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
