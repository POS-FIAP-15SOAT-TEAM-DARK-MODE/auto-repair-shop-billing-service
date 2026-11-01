package id

import (
	"regexp"
	"testing"
)

// Regex patterns for validating ID formats
var (
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	ulidPattern = regexp.MustCompile(`^[0-9A-Z]{26}$`)
)

// --- idGenerator tests ---

func TestNewIDGenerator_ReturnsGenerator(t *testing.T) {
	g := NewIDGenerator()
	if g == nil {
		t.Fatal("expected non-nil generator")
	}
}

func TestIDGenerator_ImplementsInterface(t *testing.T) {
	var _ Generator = NewIDGenerator()
}

func TestIDGenerator_NewUUID_Format(t *testing.T) {
	g := NewIDGenerator()
	id := g.NewUUID()

	if !uuidPattern.MatchString(id) {
		t.Errorf("NewUUID() = %q, want valid UUID format (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx)", id)
	}
}

func TestIDGenerator_NewUUID_Unique(t *testing.T) {
	g := NewIDGenerator()
	seen := make(map[string]struct{}, 100)

	for i := 0; i < 100; i++ {
		id := g.NewUUID()
		if _, exists := seen[id]; exists {
			t.Fatalf("NewUUID() returned duplicate value %q on iteration %d", id, i)
		}
		seen[id] = struct{}{}
	}
}

func TestIDGenerator_NewULID_Format(t *testing.T) {
	g := NewIDGenerator()
	id := g.NewULID()

	if !ulidPattern.MatchString(id) {
		t.Errorf("NewULID() = %q, want valid ULID format (26 uppercase alphanumeric chars)", id)
	}
}

func TestIDGenerator_NewULID_Unique(t *testing.T) {
	g := NewIDGenerator()
	seen := make(map[string]struct{}, 100)

	for i := 0; i < 100; i++ {
		id := g.NewULID()
		if _, exists := seen[id]; exists {
			t.Fatalf("NewULID() returned duplicate value %q on iteration %d", id, i)
		}
		seen[id] = struct{}{}
	}
}

// --- noopIdGenerator tests ---

func TestNewNoopIDGenerator_ReturnsGenerator(t *testing.T) {
	g := NewNoopIDGenerator("test-id")
	if g == nil {
		t.Fatal("expected non-nil noop generator")
	}
}

func TestNoopIDGenerator_ImplementsInterface(t *testing.T) {
	var _ Generator = NewNoopIDGenerator("test-id")
}

func TestNoopIDGenerator_NewUUID_ReturnsFixedID(t *testing.T) {
	const fixedID = "fixed-response-id"
	g := NewNoopIDGenerator(fixedID)

	got := g.NewUUID()
	if got != fixedID {
		t.Errorf("NewUUID() = %q, want %q", got, fixedID)
	}
}

func TestNoopIDGenerator_NewULID_ReturnsFixedID(t *testing.T) {
	const fixedID = "fixed-response-id"
	g := NewNoopIDGenerator(fixedID)

	got := g.NewULID()
	if got != fixedID {
		t.Errorf("NewULID() = %q, want %q", got, fixedID)
	}
}

func TestNoopIDGenerator_BothMethodsReturnSameID(t *testing.T) {
	const fixedID = "shared-id"
	g := NewNoopIDGenerator(fixedID)

	if uuid, ulid := g.NewUUID(), g.NewULID(); uuid != ulid {
		t.Errorf("expected NewUUID() and NewULID() to return the same value, got %q and %q", uuid, ulid)
	}
}

func TestNoopIDGenerator_EmptyString(t *testing.T) {
	g := NewNoopIDGenerator("")

	if got := g.NewUUID(); got != "" {
		t.Errorf("NewUUID() = %q, want empty string", got)
	}
	if got := g.NewULID(); got != "" {
		t.Errorf("NewULID() = %q, want empty string", got)
	}
}
