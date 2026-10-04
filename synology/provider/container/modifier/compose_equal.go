package modifier

import (
	"reflect"

	"gopkg.in/yaml.v3"
)

// composeSemanticallyEqual reports whether two compose documents describe the
// same structure. YAML mapping key order is not significant. A reordered
// document must not plan an update: DSM stores postgres with a different key
// order than compose-go emits, and applying that diff restarts the database
// for no change (PLAT-947).
func composeSemanticallyEqual(left, right string) (bool, error) {
	var a, b any
	if err := yaml.Unmarshal([]byte(left), &a); err != nil {
		return false, err
	}
	if err := yaml.Unmarshal([]byte(right), &b); err != nil {
		return false, err
	}
	return reflect.DeepEqual(a, b), nil
}
