package json

import (
	"io"
	"strings"
	"testing"
)

// --- test helpers ---

func toBody(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}

// --- test structs ---

type person struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type nested struct {
	Owner  person   `json:"owner"`
	Tags   []string `json:"tags"`
	Active bool     `json:"active"`
}

// --- tests ---

func TestParseJsonBodyToStruct_SimpleStruct(t *testing.T) {
	body := toBody(`{"name":"Alice","age":30}`)

	var got person
	if err := ParseJsonBodyToStruct(body, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Name != "Alice" {
		t.Errorf("Name = %q, want %q", got.Name, "Alice")
	}
	if got.Age != 30 {
		t.Errorf("Age = %d, want %d", got.Age, 30)
	}
}

func TestParseJsonBodyToStruct_NestedStruct(t *testing.T) {
	body := toBody(`{"owner":{"name":"Bob","age":25},"tags":["go","json"],"active":true}`)

	var got nested
	if err := ParseJsonBodyToStruct(body, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Owner.Name != "Bob" {
		t.Errorf("Owner.Name = %q, want %q", got.Owner.Name, "Bob")
	}
	if len(got.Tags) != 2 || got.Tags[0] != "go" || got.Tags[1] != "json" {
		t.Errorf("Tags = %v, want [go json]", got.Tags)
	}
	if !got.Active {
		t.Errorf("Active = false, want true")
	}
}

func TestParseJsonBodyToStruct_IntoMap(t *testing.T) {
	body := toBody(`{"key":"value","count":42}`)

	var got map[string]any
	if err := ParseJsonBodyToStruct(body, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got["key"] != "value" {
		t.Errorf("key = %v, want %q", got["key"], "value")
	}
}

func TestParseJsonBodyToStruct_UnknownFieldsIgnored(t *testing.T) {
	// Extra fields in the payload should be silently ignored
	body := toBody(`{"name":"Carol","age":22,"unknown_field":"ignored"}`)

	var got person
	if err := ParseJsonBodyToStruct(body, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Name != "Carol" {
		t.Errorf("Name = %q, want %q", got.Name, "Carol")
	}
}

func TestParseJsonBodyToStruct_MissingFieldsAreZeroValued(t *testing.T) {
	body := toBody(`{"name":"Dave"}`)

	var got person
	if err := ParseJsonBodyToStruct(body, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Age != 0 {
		t.Errorf("Age = %d, want 0 (zero value for missing field)", got.Age)
	}
}

func TestParseJsonBodyToStruct_EmptyObject(t *testing.T) {
	body := toBody(`{}`)

	var got person
	if err := ParseJsonBodyToStruct(body, &got); err != nil {
		t.Fatalf("unexpected error for empty object: %v", err)
	}

	if got.Name != "" || got.Age != 0 {
		t.Errorf("expected zero-value struct, got %+v", got)
	}
}

func TestParseJsonBodyToStruct_InvalidJSON_ReturnsError(t *testing.T) {
	body := toBody(`not-valid-json`)

	var got person
	if err := ParseJsonBodyToStruct(body, &got); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParseJsonBodyToStruct_EmptyBody_ReturnsError(t *testing.T) {
	body := toBody(``)

	var got person
	if err := ParseJsonBodyToStruct(body, &got); err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
}

func TestParseJsonBodyToStruct_WrongType_ReturnsError(t *testing.T) {
	// "age" is a string here but the struct expects an int
	body := toBody(`{"name":"Eve","age":"not-a-number"}`)

	var got person
	if err := ParseJsonBodyToStruct(body, &got); err == nil {
		t.Fatal("expected error for type mismatch, got nil")
	}
}

func TestParseJsonBodyToStruct_NullBody_ReturnsError(t *testing.T) {
	body := toBody(`null`)

	var got person
	// Decoding "null" into a non-pointer struct is a no-op — no error, zero value
	if err := ParseJsonBodyToStruct(body, &got); err != nil {
		t.Fatalf("unexpected error for null payload: %v", err)
	}
}

func TestParseJsonBodyToStruct_ArrayPayload(t *testing.T) {
	body := toBody(`[{"name":"Frank","age":40},{"name":"Grace","age":35}]`)

	var got []person
	if err := ParseJsonBodyToStruct(body, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "Frank" || got[1].Name != "Grace" {
		t.Errorf("unexpected values: %+v", got)
	}
}
