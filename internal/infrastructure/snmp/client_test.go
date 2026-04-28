package snmp

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"NMS1/internal/domain"

	"github.com/gosnmp/gosnmp"
)

func TestSNMPError_ErrorAndUnwrap(t *testing.T) {
	inner := errors.New("root")
	e := &SNMPError{Op: "connect", Kind: ErrorKindTimeout, Err: inner}
	if !strings.Contains(e.Error(), "connect") || !strings.Contains(e.Error(), "timeout") {
		t.Fatalf("error string: %q", e.Error())
	}
	if !errors.Is(e, inner) {
		t.Fatal("unwrap should expose inner")
	}
}

func TestGetErrorKind(t *testing.T) {
	if GetErrorKind(errors.New("plain")) != ErrorKindTransport {
		t.Fatal("plain error -> transport")
	}
	wrapped := wrapSNMPError("get", errors.New("request timeout"))
	if GetErrorKind(wrapped) != ErrorKindTimeout {
		t.Fatalf("got %v", GetErrorKind(wrapped))
	}
}

func TestClassifyErrorKind(t *testing.T) {
	cases := []struct {
		msg  string
		want ErrorKind
	}{
		{"i/o timeout", ErrorKindTimeout},
		{"no response from device", ErrorKindTimeout},
		{"authentication failure", ErrorKindAuth},
		{"unknown user name", ErrorKindAuth},
		{"no such name", ErrorKindNoSuch},
		{"no such object available", ErrorKindNoSuch},
		{"some other snmp fault", ErrorKindTransport},
	}
	for _, tc := range cases {
		if got := classifyErrorKind(errors.New(tc.msg)); got != tc.want {
			t.Fatalf("classify(%q)=%v want %v", tc.msg, got, tc.want)
		}
	}
}

func TestPduValueString(t *testing.T) {
	if pduValueString(nil) != "" {
		t.Fatal("nil -> empty")
	}
	if got := pduValueString([]byte("abc\x00")); got != "abc" {
		t.Fatalf("trim nul: %q", got)
	}
	if got := pduValueString("text"); got != "text" {
		t.Fatalf("string: %q", got)
	}
	if got := pduValueString(42); got != fmt.Sprintf("%v", 42) {
		t.Fatalf("default: %q", got)
	}
}

func TestClient_ConfigAndApplyRuntimeConfig(t *testing.T) {
	t.Parallel()
	c := New(161, 2*time.Second, 1)
	got := c.Config()
	if got.Port != 161 || got.Timeout != 2*time.Second || got.Retries != 1 {
		t.Fatalf("unexpected config: %+v", got)
	}

	c.ApplyRuntimeConfig(5*time.Second, 3)
	got = c.Config()
	if got.Timeout != 5*time.Second || got.Retries != 3 {
		t.Fatalf("unexpected config after apply: %+v", got)
	}
}

func TestClient_AuthAndPrivProtocolMapping(t *testing.T) {
	t.Parallel()
	c := New(161, time.Second, 0)

	if got := c.authProtocol("sha256"); got != gosnmp.SHA256 {
		t.Fatalf("auth sha256: got %v", got)
	}
	if got := c.authProtocol("bad"); got != gosnmp.NoAuth {
		t.Fatalf("auth bad: got %v", got)
	}

	if got := c.privProtocol("aes"); got != gosnmp.AES {
		t.Fatalf("priv aes: got %v", got)
	}
	if got := c.privProtocol("aes256"); got != gosnmp.AES256 {
		t.Fatalf("priv aes256: got %v", got)
	}
	if got := c.privProtocol("bad"); got != gosnmp.NoPriv {
		t.Fatalf("priv bad: got %v", got)
	}
}

func TestClient_NewV3ConnValidation(t *testing.T) {
	t.Parallel()
	c := New(161, time.Second, 0)

	t.Run("missing username", func(t *testing.T) {
		_, err := c.newV3Conn(&domain.Device{IP: "10.0.0.1", SNMPVersion: "v3"})
		if err == nil || !strings.Contains(err.Error(), "username") {
			t.Fatalf("expected username error, got %v", err)
		}
	})

	t.Run("priv requires auth", func(t *testing.T) {
		_, err := c.newV3Conn(&domain.Device{
			IP:          "10.0.0.1",
			SNMPVersion: "v3",
			Community:   "user",
			PrivProto:   "AES",
			PrivPass:    "p",
		})
		if err == nil || !strings.Contains(err.Error(), "priv requires auth") {
			t.Fatalf("expected priv requires auth error, got %v", err)
		}
	})

	t.Run("unsupported auth proto", func(t *testing.T) {
		_, err := c.newV3Conn(&domain.Device{
			IP:          "10.0.0.1",
			SNMPVersion: "v3",
			Community:   "user",
			AuthProto:   "BAD",
			AuthPass:    "p",
		})
		if err == nil || !strings.Contains(err.Error(), "unsupported auth_proto") {
			t.Fatalf("expected unsupported auth_proto error, got %v", err)
		}
	})

	t.Run("unsupported priv proto", func(t *testing.T) {
		_, err := c.newV3Conn(&domain.Device{
			IP:          "10.0.0.1",
			SNMPVersion: "v3",
			Community:   "user",
			AuthProto:   "SHA",
			AuthPass:    "p",
			PrivProto:   "BAD",
			PrivPass:    "p2",
		})
		if err == nil || !strings.Contains(err.Error(), "unsupported priv_proto") {
			t.Fatalf("expected unsupported priv_proto error, got %v", err)
		}
	})
}
