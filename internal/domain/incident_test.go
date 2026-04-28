package domain

import "testing"

func TestJSONPayloadMarshalJSON(t *testing.T) {
	t.Parallel()
	var empty JSONPayload
	b, err := empty.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON empty: %v", err)
	}
	if string(b) != "null" {
		t.Fatalf("MarshalJSON empty: got %q want null", string(b))
	}

	p := JSONPayload(`{"a":1}`)
	b, err = p.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON payload: %v", err)
	}
	if string(b) != `{"a":1}` {
		t.Fatalf("MarshalJSON payload: got %q", string(b))
	}
}

func TestJSONPayloadUnmarshalJSON(t *testing.T) {
	t.Parallel()
	var p JSONPayload
	if err := p.UnmarshalJSON([]byte(`{"x":"y"}`)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if string(p) != `{"x":"y"}` {
		t.Fatalf("UnmarshalJSON: got %q", string(p))
	}

	// nil receiver should be a no-op, not panic.
	var nilPayload *JSONPayload
	if err := nilPayload.UnmarshalJSON([]byte(`{}`)); err != nil {
		t.Fatalf("UnmarshalJSON nil receiver: %v", err)
	}
}
