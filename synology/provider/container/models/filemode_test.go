package models

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/synology-community/terraform-provider-synology/synology/models/composetypes"
)

func TestParseFileMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want uint32
	}{
		{raw: "0400", want: 0o400},
		{raw: "400", want: 0o400},
		{raw: "0o400", want: 0o400},
		{raw: "0O400", want: 0o400},
		{raw: " 0400 ", want: 0o400},
		{raw: "0660", want: 0o660},
		{raw: "660", want: 0o660},
		{raw: "0444", want: 0o444},
		{raw: "0755", want: 0o755},
		{raw: "0o755", want: 0o755},
		{raw: "777", want: 0o777},
		{raw: "644", want: 0o644},
		// Octal 256 is not decimal 256. Decimal 256 is written "0400".
		{raw: "256", want: 0o256},
		{raw: "0", want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := parseFileMode(tc.raw)
			if err != nil {
				t.Fatalf("parseFileMode(%q) error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("parseFileMode(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseFileModeRejectsNonOctal(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "   ", "089", "4008", "0o", "0O", "0x100", "0b111", "400.0"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if _, err := parseFileMode(raw); err == nil {
				t.Fatalf("parseFileMode(%q) succeeded, want an error", raw)
			}
		})
	}
}

func TestServiceSecretAndConfigModesRenderOctal(t *testing.T) {
	t.Parallel()

	// Spellings that must not collapse to a one-off replacement of "0400".
	cases := []struct {
		mode string
		want uint32
	}{
		{mode: "0400", want: 256},
		{mode: "400", want: 256},
		{mode: "0o400", want: 256},
		{mode: "0660", want: 0o660},
		{mode: "777", want: 0o777},
		{mode: "0444", want: 0o444},
	}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			t.Parallel()

			svc := Service{
				Configs: fileModeList(tc.mode),
				Secrets: fileModeList(tc.mode),
			}
			var got composetypes.ServiceConfig
			if diags := svc.AsComposeConfig(t.Context(), &got); diags.HasError() {
				t.Fatalf("AsComposeConfig() diagnostics: %s", diags)
			}
			assertMode(t, "config", got.Configs[0].Mode, tc.want)
			assertMode(t, "secret", got.Secrets[0].Mode, tc.want)

			project := composetypes.Project{
				Services: composetypes.Services{"app": got},
			}
			raw, err := project.MarshalYAML()
			if err != nil {
				t.Fatalf("MarshalYAML() error: %v", err)
			}
			rendered := string(raw)
			needle := fmt.Sprintf("mode: %d\n", tc.want)
			if gotCount := strings.Count(rendered, needle); gotCount != 2 {
				t.Errorf(
					"rendered compose has %d %q lines, want 2 (config and secret):\n%s",
					gotCount,
					needle,
					rendered,
				)
			}
			// Decimal 400 is the old base-10 reading of "0400" (octal 0620).
			if tc.want == 256 && strings.Contains(rendered, "mode: 400\n") {
				t.Errorf("rendered compose still has decimal mode: 400:\n%s", rendered)
			}
		})
	}
}

func TestServiceFileModeRejectsNonOctalOnSecretsAndConfigs(t *testing.T) {
	t.Parallel()

	svc := Service{
		Configs: fileModeList("089"),
		Secrets: fileModeList("4008"),
	}
	var got composetypes.ServiceConfig
	diags := svc.AsComposeConfig(t.Context(), &got)
	if !diags.HasError() {
		t.Fatal("AsComposeConfig() succeeded, want errors for both non-octal modes")
	}
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics, want 2: %s", len(diags), diags)
	}
	if got.Configs[0].Mode != nil || got.Secrets[0].Mode != nil {
		t.Fatalf(
			"invalid modes were stored: config %v secret %v",
			got.Configs[0].Mode,
			got.Secrets[0].Mode,
		)
	}
}

func TestServiceFileModeOmitsNullAndUnknown(t *testing.T) {
	t.Parallel()

	for _, mode := range []types.String{types.StringNull(), types.StringUnknown()} {
		ref := types.ObjectValueMust(ServiceConfig{}.AttrType(), map[string]attr.Value{
			"source": types.StringValue("src"),
			"target": types.StringValue("/dst"),
			"uid":    types.StringNull(),
			"gid":    types.StringNull(),
			"mode":   mode,
		})
		list := types.ListValueMust(ServiceConfig{}.ModelType(), []attr.Value{ref})
		svc := Service{Configs: list, Secrets: list}
		var got composetypes.ServiceConfig
		if diags := svc.AsComposeConfig(t.Context(), &got); diags.HasError() {
			t.Fatalf("AsComposeConfig() diagnostics: %s", diags)
		}
		if got.Configs[0].Mode != nil || got.Secrets[0].Mode != nil {
			t.Fatalf(
				"unset mode was stored: config %v secret %v",
				got.Configs[0].Mode,
				got.Secrets[0].Mode,
			)
		}
	}
}

func fileModeList(mode string) types.List {
	ref := types.ObjectValueMust(ServiceConfig{}.AttrType(), map[string]attr.Value{
		"source": types.StringValue("src"),
		"target": types.StringValue("/dst"),
		"uid":    types.StringNull(),
		"gid":    types.StringNull(),
		"mode":   types.StringValue(mode),
	})
	return types.ListValueMust(ServiceConfig{}.ModelType(), []attr.Value{ref})
}

func assertMode(t *testing.T, kind string, got *uint32, want uint32) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s mode is nil, want %d", kind, want)
	}
	if *got != want {
		t.Errorf("%s mode = %d, want %d", kind, *got, want)
	}
}
