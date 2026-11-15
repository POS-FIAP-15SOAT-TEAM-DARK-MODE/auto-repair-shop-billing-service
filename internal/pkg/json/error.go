package json

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var (
	ErrWrongPayloadFormat   = fmt.Errorf("invalid payload")
	ErrJSONSyntax           = fmt.Errorf("invalid JSON syntax")
	ErrJSONType             = fmt.Errorf("JSON type mismatch")
	ErrJSONInvalidUnmarshal = fmt.Errorf("invalid unmarshal target")
	ErrJSONUnsupportedType  = fmt.Errorf("unsupported JSON type")
	ErrJSONUnsupportedValue = fmt.Errorf("unsupported JSON value")
	ErrJSONUnexpectedEOF    = fmt.Errorf("unexpected end of JSON input")
	ErrJSONEmptyBody        = fmt.Errorf("JSON body is empty")
)

func mapJSONErrorType(err error) error {
	var syntaxError *json.SyntaxError
	var unmarshalTypeError *json.UnmarshalTypeError
	var invalidUnmarshalError *json.InvalidUnmarshalError
	var unsupportedTypeError *json.UnsupportedTypeError
	var unsupportedValueError *json.UnsupportedValueError
	switch {
	case errors.As(err, &syntaxError):
		return ErrJSONSyntax
	case errors.As(err, &unmarshalTypeError):
		return ErrJSONType
	case errors.As(err, &invalidUnmarshalError):
		return ErrJSONInvalidUnmarshal
	case errors.As(err, &unsupportedTypeError):
		return ErrJSONUnsupportedType
	case errors.As(err, &unsupportedValueError):
		return ErrJSONUnsupportedValue
	}
	return nil
}

func CheckJsonError(err error) error {
	if err == nil {
		return nil
	}

	if mapped := mapJSONErrorType(err); mapped != nil {
		return mapped
	}

	if errors.Is(err, io.EOF) {
		return ErrJSONEmptyBody
	}

	if errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrJSONUnexpectedEOF
	}

	switch err.Error() {
	case "unexpected EOF", "unexpected end of JSON input":
		return ErrJSONUnexpectedEOF
	case "EOF":
		return ErrJSONEmptyBody
	}

	return ErrWrongPayloadFormat
}
