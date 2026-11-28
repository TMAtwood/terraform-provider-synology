package models

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// parseFileMode reads a service secret or config mode string as octal
// permission bits.
//
// The Compose specification defines this field as octal notation (0400, 0444).
// compose-go's FileMode parses every string with base 8, so "0400", "400", and
// "0660" are permissions, not decimal integers. A Terraform string cannot keep
// a YAML 1.1 leading-zero integer, which is why mode = "0400" used to be read
// with base 10 and rendered as mode: 400 (octal 0620).
//
// Spellings, all octal:
//   - "0400", "400", "0660", "777" (optional leading zero, digits 0-7)
//   - "0o400" or "0O400" (explicit octal prefix)
//
// The returned value is the decimal permission bits. Callers store it in the
// uint32 compose field, which YAML emits as an integer. Octal 0400 is 256,
// matching the compose DSM already stores. A digit 8 or 9 is rejected. "256"
// is octal 256 (decimal 174), not a decimal spelling of 0400.
func parseFileMode(raw string) (uint32, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("file mode is empty")
	}
	if strings.HasPrefix(s, "0o") || strings.HasPrefix(s, "0O") {
		s = s[2:]
		if s == "" {
			return 0, fmt.Errorf("file mode %q has an octal prefix and no digits", raw)
		}
	}
	n, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf(
			"file mode %q is not an octal permission: use digits 0-7, with an optional leading 0 or 0o prefix",
			raw,
		)
	}
	return uint32(n), nil
}

// assignFileMode writes a parsed secret or config mode into dst.
// A null or unknown mode is left unset. An invalid mode is reported and left unset.
func assignFileMode(raw types.String, dst **uint32, diags *diag.Diagnostics) {
	if raw.IsNull() || raw.IsUnknown() {
		return
	}
	mode, err := parseFileMode(raw.ValueString())
	if err != nil {
		diags.AddError("Invalid service file mode", err.Error())
		return
	}
	*dst = &mode
}
