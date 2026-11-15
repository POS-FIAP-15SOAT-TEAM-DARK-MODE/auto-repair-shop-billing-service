package json_test

import (
	"encoding/json"
	"errors"
	"io"
	"testing"

	pkgJson "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/json"
	"github.com/stretchr/testify/assert"
)

func TestCheckJsonError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected error
	}{
		{"NilError", nil, nil},
		{"SyntaxError", &json.SyntaxError{}, pkgJson.ErrJSONSyntax},
		{"UnmarshalTypeError", &json.UnmarshalTypeError{}, pkgJson.ErrJSONType},
		{"InvalidUnmarshalError", &json.InvalidUnmarshalError{}, pkgJson.ErrJSONInvalidUnmarshal},
		{"UnsupportedTypeError", &json.UnsupportedTypeError{}, pkgJson.ErrJSONUnsupportedType},
		{"UnsupportedValueError", &json.UnsupportedValueError{}, pkgJson.ErrJSONUnsupportedValue},
		{"EOF", io.EOF, pkgJson.ErrJSONEmptyBody},
		{"UnexpectedEOF", io.ErrUnexpectedEOF, pkgJson.ErrJSONUnexpectedEOF},
		{"StringEOF", errors.New("EOF"), pkgJson.ErrJSONEmptyBody},
		{"StringUnexpectedEOF", errors.New("unexpected EOF"), pkgJson.ErrJSONUnexpectedEOF},
		{"UnknownError", errors.New("unknown"), pkgJson.ErrWrongPayloadFormat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pkgJson.CheckJsonError(tt.err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
