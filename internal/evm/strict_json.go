package evm

import (
	"bytes"
	"encoding/json"
	"io"
)

const (
	MaxStrictJSONBytes    = 256 << 10
	MaxStrictJSONDepth    = 32
	MaxStrictJSONArrayLen = 256
)

func validateStrictJSON(data []byte, maxBytes, maxDepth, maxArray int) error {
	if len(data) == 0 || len(data) > maxBytes {
		return invalidIntent("strict JSON size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return &EngineError{Code: ErrorInvalidIntent, Field: "strict JSON", Cause: err}
	}
	delimiter, ok := token.(json.Delim)
	if !ok || (delimiter != '{' && delimiter != '[') {
		return invalidIntent("strict JSON top-level container")
	}
	if delimiter == '{' {
		if err := walkStrictJSONObject(decoder, 1, maxDepth, maxArray); err != nil {
			return err
		}
	} else {
		if err := walkStrictJSONArray(decoder, 1, maxDepth, maxArray); err != nil {
			return err
		}
	}
	return rejectStrictJSONTrailing(decoder)
}

func rejectStrictJSONTrailing(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return invalidIntent("strict JSON trailing data")
	}
	return nil
}

func walkStrictJSONArray(decoder *json.Decoder, depth, maxDepth, maxArray int) error {
	if depth > maxDepth {
		return invalidIntent("strict JSON depth")
	}
	count := 0
	for decoder.More() {
		if count >= maxArray {
			return invalidIntent("strict JSON array budget")
		}
		if err := walkStrictJSONValue(decoder, depth+1, maxDepth, maxArray); err != nil {
			return err
		}
		count++
	}
	closing, err := decoder.Token()
	if err != nil {
		return &EngineError{Code: ErrorInvalidIntent, Field: "strict JSON", Cause: err}
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != ']' {
		return invalidIntent("strict JSON array end")
	}
	return nil
}

func walkStrictJSONObject(decoder *json.Decoder, depth, maxDepth, maxArray int) error {
	if depth > maxDepth {
		return invalidIntent("strict JSON depth")
	}
	seen := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return &EngineError{Code: ErrorInvalidIntent, Field: "strict JSON", Cause: err}
		}
		key, ok := keyToken.(string)
		if !ok {
			return invalidIntent("strict JSON key")
		}
		if _, duplicate := seen[key]; duplicate {
			return invalidIntent("strict JSON duplicate key")
		}
		seen[key] = struct{}{}
		if err := walkStrictJSONValue(decoder, depth+1, maxDepth, maxArray); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return &EngineError{Code: ErrorInvalidIntent, Field: "strict JSON", Cause: err}
	}
	if delimiter, ok := closing.(json.Delim); !ok || delimiter != '}' {
		return invalidIntent("strict JSON object end")
	}
	return nil
}

func walkStrictJSONValue(decoder *json.Decoder, depth, maxDepth, maxArray int) error {
	if depth > maxDepth {
		return invalidIntent("strict JSON depth")
	}
	token, err := decoder.Token()
	if err != nil {
		return &EngineError{Code: ErrorInvalidIntent, Field: "strict JSON", Cause: err}
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		return walkStrictJSONObject(decoder, depth, maxDepth, maxArray)
	case '[':
		count := 0
		for decoder.More() {
			if count >= maxArray {
				return invalidIntent("strict JSON array budget")
			}
			if err := walkStrictJSONValue(decoder, depth+1, maxDepth, maxArray); err != nil {
				return err
			}
			count++
		}
		closing, err := decoder.Token()
		if err != nil {
			return &EngineError{Code: ErrorInvalidIntent, Field: "strict JSON", Cause: err}
		}
		if arrayEnd, ok := closing.(json.Delim); !ok || arrayEnd != ']' {
			return invalidIntent("strict JSON array end")
		}
		return nil
	default:
		return invalidIntent("strict JSON container")
	}
}
