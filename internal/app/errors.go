package app

import "errors"

// Generic Errors
var (
	ErrDataConflict  = errors.New("resource already exists")
	ErrDataViolation = errors.New("data violates business constraints")
	ErrInfraConflict = errors.New("something went wrong with our resources, try again")
)
