package evaluation

import (
	"encoding/json"
	"fmt"

	"github.com/croge130/archipelago/typedvalue"
	"github.com/croge130/archipelago/vitals/structure"
)

// NormalizeAndValidateValue enforces def.ValueMetadata against value
// when the definition declares one. Enforcement is mandatory, not a
// separate opt-in setting: defining ValueMetadata at all is already
// the declaration of intent for what this vital's value is, the same
// reasoning that lets Policy's own typeconstraints.Set.Check run
// unconditionally whenever a PolicyDefinition carries constraints
// (policy/facade/policy_instance.go's SetPolicyInstance) rather than
// behind a second "should I enforce this" flag. The lever stays where
// it already was: a Definition author who doesn't want validation
// leaves ValueMetadata nil, or uses AppValueMetadata instead.
//
// def.ValueMetadata == nil, or an empty/absent value, both pass
// through unchanged — this only engages when there's both a declared
// shape and a value to check it against. A Reading with no Value at
// all is not itself a violation of a definition that declares one;
// that's a presence rule, a different question than shape validation,
// and not one this function answers.
//
// On success, returns value's canonical form (coerced/trimmed per
// typedvalue.NormalizeValue, then re-marshaled), not the caller's raw
// bytes — mirroring Policy's own "store the normalized value, not the
// raw input" behavior, so a Reading's stored Value is stable for any
// consumer that already knows the Definition's ValueMetadata shape.
func NormalizeAndValidateValue(def structure.Definition, value json.RawMessage) (json.RawMessage, error) {
	if def.ValueMetadata == nil || len(value) == 0 {
		return value, nil
	}

	var decoded any
	if err := json.Unmarshal(value, &decoded); err != nil {
		return nil, fmt.Errorf("evaluation: value: invalid JSON: %w", err)
	}

	normalized, err := typedvalue.NormalizeValue(*def.ValueMetadata, decoded)
	if err != nil {
		return nil, fmt.Errorf("evaluation: value: %w", err)
	}
	if def.ValueMetadata.Storage == typedvalue.StorageEnum {
		if err := typedvalue.ValidateValue(*def.ValueMetadata, normalized); err != nil {
			return nil, fmt.Errorf("evaluation: value: %w", err)
		}
	}

	out, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("evaluation: value: marshal normalized value: %w", err)
	}
	return out, nil
}
