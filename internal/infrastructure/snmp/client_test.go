package snmp

import (
	"errors"
	"fmt"
	"strings"
	"testing"
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
