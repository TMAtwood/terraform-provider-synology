package modifier

import (
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

// composeSemanticallyEqual reports whether two compose documents describe the
// same values. Mapping key order is ignored. Sequence order is not, because
// command arguments and published ports are ordered.
//
// Scalars use YAML 1.1 rules. Unquoted 0400 is octal 256, so it matches the
// decimal mode DSM stores for a file mode of 0400. Unquoted 400 is decimal
// 400 (octal 0620) and does not match. Treating 400 as octal would hide a
// permission change.
func composeSemanticallyEqual(stored, rendered string) (bool, error) {
	storedValue, err := decodeCompose(stored)
	if err != nil {
		return false, fmt.Errorf("decode stored compose: %w", err)
	}
	renderedValue, err := decodeCompose(rendered)
	if err != nil {
		return false, fmt.Errorf("decode rendered compose: %w", err)
	}
	return reflect.DeepEqual(storedValue, renderedValue), nil
}

func decodeCompose(content string) (any, error) {
	var value any
	if err := yaml.Unmarshal([]byte(content), &value); err != nil {
		return nil, err
	}
	return value, nil
}
