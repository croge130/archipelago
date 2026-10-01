package dbstore

import (
	"encoding/json"
	"fmt"

	"github.com/croge130/archipelago/typeconstraints"
	"github.com/croge130/archipelago/typedvalue"
)

func unmarshalValueType(data []byte) (typedvalue.Definition, error) {
	var d typedvalue.Definition
	if err := json.Unmarshal(data, &d); err != nil {
		return typedvalue.Definition{}, fmt.Errorf("dbstore: unmarshal value_type: %w", err)
	}
	return d, nil
}

func unmarshalConstraints(data []byte) (typeconstraints.Set, error) {
	var s typeconstraints.Set
	if err := json.Unmarshal(data, &s); err != nil {
		return typeconstraints.Set{}, fmt.Errorf("dbstore: unmarshal constraints: %w", err)
	}
	return s, nil
}

// normalizeStoredValue re-derives the exact Go type
// typedvalue.NormalizeValue would produce (int64, not jsonb's default
// float64) after a value has round-tripped through storage.
func normalizeStoredValue(valueType typedvalue.Definition, data []byte) (any, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("dbstore: unmarshal value: %w", err)
	}
	normalized, err := typedvalue.NormalizeValue(valueType, raw)
	if err != nil {
		return nil, fmt.Errorf("dbstore: normalize stored value: %w", err)
	}
	return normalized, nil
}
