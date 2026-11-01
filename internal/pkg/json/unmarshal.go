package json

import (
	"encoding/json"
	"io"
)

func ParseJsonBodyToStruct[T any](body io.ReadCloser, output *T) error {
	if err := json.NewDecoder(body).Decode(output); err != nil {
		return CheckJsonError(err)
	}
	return nil
}
