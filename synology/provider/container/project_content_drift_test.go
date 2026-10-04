package container

import (
	"bytes"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/synology-community/terraform-provider-synology/synology/provider/container/models"
	"github.com/synology-community/terraform-provider-synology/synology/provider/container/modifier"
	"gopkg.in/yaml.v3"
)

// planContent runs the content plan modifier the way a refresh does: content
// is omitted from config, and state holds the compose DSM last stored.
func planContent(t *testing.T, plan tfsdk.Plan, state types.String) string {
	t.Helper()

	req := planmodifier.StringRequest{
		Path:        path.Root("content"),
		Plan:        plan,
		ConfigValue: types.StringNull(),
		StateValue:  state,
		PlanValue:   state,
	}
	resp := planmodifier.StringResponse{PlanValue: req.PlanValue}
	modifier.UseSchemaForUnknownContent().PlanModifyString(t.Context(), req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("PlanModifyString() diagnostics: %s", resp.Diagnostics)
	}
	if resp.PlanValue.IsNull() || resp.PlanValue.IsUnknown() {
		t.Fatalf("planned content is %s, want a known string", resp.PlanValue)
	}
	return resp.PlanValue.ValueString()
}

func setServiceAttr(t *testing.T, plan *tfsdk.Plan, name string, value any) {
	t.Helper()
	diags := plan.SetAttribute(
		t.Context(),
		path.Root("services").AtMapKey("app").AtName(name),
		value,
	)
	if diags.HasError() {
		t.Fatalf("set services.app.%s: %s", name, diags)
	}
}

// TestContentPlan_OctalFileModeDoesNotPlanAsDecimal400 is the PLAT-947 mode
// regression. mode = "0400" is octal. DSM stores that permission as decimal
// 256. Reading the string as decimal plans mode: 400, which is octal 0620.
func TestContentPlan_OctalFileModeDoesNotPlanAsDecimal400(t *testing.T) {
	ctx := t.Context()
	plan := planWithServiceImage(t, ctx, "forgejo:1")
	secret := types.ObjectValueMust(models.ServiceConfig{}.AttrType(), map[string]attr.Value{
		"source": types.StringValue("forgejo-db-password"),
		"target": types.StringValue("/data/gitea/conf/app.ini.secret"),
		"uid":    types.StringNull(),
		"gid":    types.StringNull(),
		"mode":   types.StringValue("0400"),
	})
	secrets := types.ListValueMust(models.ServiceConfig{}.ModelType(), []attr.Value{secret})
	setServiceAttr(t, &plan, "secrets", secrets)

	planned := planContent(t, plan, types.StringNull())
	if strings.Contains(planned, "mode: 400\n") || strings.Contains(planned, "mode: 400\r\n") {
		t.Fatalf("mode = \"0400\" planned as decimal 400 (octal 0620):\n%s", planned)
	}
	if !strings.Contains(planned, "mode: 256\n") {
		t.Fatalf("planned compose missing mode: 256, the decimal form of octal 0400:\n%s", planned)
	}

	// Same permission, keys in the order DSM stored. Must not plan a change.
	stored := reorderMappingKeys(t, planned)
	if stored == planned {
		t.Fatalf("reorder left the compose unchanged:\n%s", planned)
	}
	if got := planContent(t, plan, types.StringValue(stored)); got != stored {
		t.Errorf("equivalent compose planned a change\nplanned:\n%s\nstored:\n%s", got, stored)
	}

	// Decimal 400 is a different mode. Keep planning the correction to 256.
	dangerous := strings.ReplaceAll(stored, "mode: 256", "mode: 400")
	if dangerous == stored {
		t.Fatalf("failed to plant decimal mode 400 in stored compose:\n%s", stored)
	}
	got := planContent(t, plan, types.StringValue(dangerous))
	if got == dangerous || strings.Contains(got, "mode: 400") {
		t.Fatalf("decimal mode 400 was kept; octal 0400 must plan as 256:\n%s", got)
	}
	if !strings.Contains(got, "mode: 256") {
		t.Fatalf("planned compose missing mode: 256:\n%s", got)
	}
}

// TestContentPlan_ReorderedKeysDoNotPlanAChange is the PLAT-947 postgres
// regression. container_name, deploy, ports, command, and healthcheck moved,
// and no value changed. That must not plan an update. A changed value still must.
func TestContentPlan_ReorderedKeysDoNotPlanAChange(t *testing.T) {
	ctx := t.Context()
	plan := planWithServiceImage(t, ctx, "postgres:16")
	setServiceAttr(t, &plan, "container_name", "db")
	setServiceAttr(t, &plan, "command", []string{"postgres"})
	setServiceAttr(t, &plan, "replicas", int64(1))
	setServiceAttr(t, &plan, "healthcheck", healthCheckValue(t))
	setServiceAttr(t, &plan, "ports", portListValue())

	planned := planContent(t, plan, types.StringNull())
	for _, key := range []string{"container_name:", "deploy:", "ports:", "command:", "healthcheck:"} {
		if !strings.Contains(planned, key) {
			t.Fatalf("rendered compose missing %s\n%s", key, planned)
		}
	}

	stored := reorderMappingKeys(t, planned)
	if stored == planned {
		t.Fatalf("reorder left the compose unchanged:\n%s", planned)
	}
	if got := planContent(t, plan, types.StringValue(stored)); got != stored {
		t.Errorf("reordered keys planned a change\nplanned:\n%s\nstored:\n%s", got, stored)
	}

	drifted := strings.Replace(stored, "postgres:16", "postgres:17", 1)
	if drifted == stored {
		t.Fatalf("failed to change the stored image:\n%s", stored)
	}
	got := planContent(t, plan, types.StringValue(drifted))
	if got == drifted {
		t.Fatalf("a changed image was treated as key-order noise:\n%s", got)
	}
	if !strings.Contains(got, "postgres:16") {
		t.Fatalf("planned compose lost the configured image:\n%s", got)
	}
}

func TestContentPlan_InvalidStoredComposePlansRendered(t *testing.T) {
	ctx := t.Context()
	plan := planWithServiceImage(t, ctx, "nginx:new")
	got := planContent(t, plan, types.StringValue("services: [\n"))
	if !strings.Contains(got, "nginx:new") {
		t.Fatalf("invalid stored compose dropped the rendered plan:\n%s", got)
	}
}

func healthCheckValue(t *testing.T) types.Object {
	t.Helper()
	testCmd := types.ListValueMust(types.StringType, []attr.Value{
		types.StringValue("CMD"),
		types.StringValue("pg_isready"),
	})
	obj, diags := types.ObjectValue(models.HealthCheck{}.AttrType(), map[string]attr.Value{
		"test":           testCmd,
		"interval":       timetypes.NewGoDurationNull(),
		"timeout":        timetypes.NewGoDurationNull(),
		"start_interval": timetypes.NewGoDurationNull(),
		"start_period":   timetypes.NewGoDurationNull(),
		"retries":        types.Int64Null(),
	})
	if diags.HasError() {
		t.Fatalf("healthcheck object: %s", diags)
	}
	return obj
}

func portListValue() types.List {
	port := types.ObjectValueMust(models.Port{}.AttrType(), map[string]attr.Value{
		"name":         types.StringNull(),
		"target":       types.Int64Value(5432),
		"published":    types.StringValue("5432"),
		"protocol":     types.StringValue("tcp"),
		"app_protocol": types.StringNull(),
		"mode":         types.StringNull(),
		"host_ip":      types.StringNull(),
	})
	return types.ListValueMust(models.Port{}.ModelType(), []attr.Value{port})
}

// reorderMappingKeys reverses every mapping in a compose document and encodes
// it again. Values stay put. The result is what a string compare would call drift.
func reorderMappingKeys(t *testing.T, src string) string {
	t.Helper()

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("unmarshal compose: %v", err)
	}
	reverseMappings(&doc)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		t.Fatalf("encode compose: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close compose encoder: %v", err)
	}
	return buf.String()
}

func reverseMappings(node *yaml.Node) {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			reverseMappings(child)
		}
	case yaml.MappingNode:
		type pair struct {
			key   *yaml.Node
			value *yaml.Node
		}
		pairs := make([]pair, 0, len(node.Content)/2)
		for i := 0; i+1 < len(node.Content); i += 2 {
			reverseMappings(node.Content[i+1])
			pairs = append(pairs, pair{key: node.Content[i], value: node.Content[i+1]})
		}
		for i, j := 0, len(pairs)-1; i < j; i, j = i+1, j-1 {
			pairs[i], pairs[j] = pairs[j], pairs[i]
		}
		node.Content = node.Content[:0]
		for _, p := range pairs {
			node.Content = append(node.Content, p.key, p.value)
		}
	}
}
