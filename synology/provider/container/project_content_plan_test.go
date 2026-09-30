package container

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/synology-community/terraform-provider-synology/synology/provider/container/models"
	"github.com/synology-community/terraform-provider-synology/synology/provider/container/modifier"
)

// planWithServiceImage builds a plan for synology_container_project whose
// config sets services.app.image and omits content -- the shape every
// project in ap100298 uses.
func planWithServiceImage(t *testing.T, ctx context.Context, image string) tfsdk.Plan {
	t.Helper()

	var schemaResp resource.SchemaResponse
	(&ProjectResource{}).Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("Schema() diagnostics: %s", schemaResp.Diagnostics)
	}

	plan := tfsdk.Plan{
		Schema: schemaResp.Schema,
		Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil),
	}
	diags := plan.SetAttribute(ctx, path.Root("name"), "runner")
	diags.Append(plan.SetAttribute(
		ctx,
		path.Root("services").AtMapKey("app").AtName("image"),
		image,
	)...)
	if diags.HasError() {
		t.Fatalf("plan.SetAttribute() diagnostics: %s", diags)
	}
	return plan
}

// TestContentPlan_ServicesChangeOverridesStoredContent is the PLAT-902
// regression test. With services configured and content omitted, the planned
// content must be rendered from services. Carrying the stored content forward
// made Update send DSM the old compose, so a changed image never took effect
// and drift made outside OpenTofu never showed in a plan.
func TestContentPlan_ServicesChangeOverridesStoredContent(t *testing.T) {
	ctx := context.Background()

	plan := planWithServiceImage(t, ctx, "nginx:new")

	var model models.ProjectResourceModel
	if diags := plan.Get(ctx, &model); diags.HasError() {
		t.Fatalf("plan.Get() diagnostics: %s", diags)
	}
	var want string
	if diags := model.ConfigRaw(ctx, &want); diags.HasError() {
		t.Fatalf("ConfigRaw() diagnostics: %s", diags)
	}

	stored := types.StringValue("services:\n  app:\n    image: nginx:old\n")
	req := planmodifier.StringRequest{
		Path:        path.Root("content"),
		Plan:        plan,
		ConfigValue: types.StringNull(),
		StateValue:  stored,
		PlanValue:   stored, // what UseStateForUnknown hands on
	}
	resp := planmodifier.StringResponse{PlanValue: req.PlanValue}

	modifier.UseSchemaForUnknownContent().PlanModifyString(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("PlanModifyString() diagnostics: %s", resp.Diagnostics)
	}

	if got := resp.PlanValue.ValueString(); got != want {
		t.Errorf("planned content = %q, want content rendered from services %q", got, want)
	}
}

// TestContentPlan_ImportFreezeKeepsStoredContent guards PLAT-552: with
// neither content nor services configured, the stored compose is kept rather
// than rewritten to an empty services document.
func TestContentPlan_ImportFreezeKeepsStoredContent(t *testing.T) {
	ctx := context.Background()

	var schemaResp resource.SchemaResponse
	(&ProjectResource{}).Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	plan := tfsdk.Plan{
		Schema: schemaResp.Schema,
		Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil),
	}
	if diags := plan.SetAttribute(ctx, path.Root("name"), "imported"); diags.HasError() {
		t.Fatalf("plan.SetAttribute() diagnostics: %s", diags)
	}

	stored := types.StringValue("services:\n  app:\n    image: nginx:old\n")
	req := planmodifier.StringRequest{
		Path:        path.Root("content"),
		Plan:        plan,
		ConfigValue: types.StringNull(),
		StateValue:  stored,
		PlanValue:   stored,
	}
	resp := planmodifier.StringResponse{PlanValue: req.PlanValue}

	modifier.UseSchemaForUnknownContent().PlanModifyString(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("PlanModifyString() diagnostics: %s", resp.Diagnostics)
	}

	if !resp.PlanValue.Equal(stored) {
		t.Errorf("planned content = %s, want stored content %s kept", resp.PlanValue, stored)
	}
}
