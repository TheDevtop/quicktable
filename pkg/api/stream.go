package api

import (
	"encoding/json"
	"io"
)

// Decode json from a reader
func DecodeStream[T any](stream io.Reader) (T, error) {
	var ptr = new(T)
	buf, err := io.ReadAll(stream)
	if err != nil {
		return *ptr, err
	}
	if err = json.Unmarshal(buf, ptr); err != nil {
		return *ptr, err
	}
	return *ptr, nil
}

// Encode json from a writer
func EncodeStream(stream io.Writer, obj any) error {
	buf, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	stream.Write(buf)
	return nil
}
