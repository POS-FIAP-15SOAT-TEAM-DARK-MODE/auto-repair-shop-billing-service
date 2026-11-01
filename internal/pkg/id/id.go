package id

import (
	"github.com/google/uuid"
	"github.com/oklog/ulid/v2"
)

type Generator interface {
	NewUUID() string
	NewULID() string
}

func NewIDGenerator() *idGenerator {
	return &idGenerator{}
}

type idGenerator struct{}

func (u idGenerator) NewUUID() string {
	return uuid.New().String()
}

func (u idGenerator) NewULID() string {
	return ulid.Make().String()
}

func NewNoopIDGenerator(responseId string) *noopIdGenerator {
	return &noopIdGenerator{responseId}
}

type noopIdGenerator struct {
	responseId string
}

func (nu noopIdGenerator) NewUUID() string {
	return nu.responseId
}

func (nu noopIdGenerator) NewULID() string {
	return nu.responseId
}
