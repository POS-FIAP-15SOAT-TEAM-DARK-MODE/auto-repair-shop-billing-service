package web

// Validator is implemented by request structs that perform DTO-level validation
// (fields that have no direct equivalent in the domain layer).
type Validator interface {
	Validate() error
}
