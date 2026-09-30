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
	"github.com/synology-community/go-synology/pkg/api/docker"
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

// stubProjectAPI implements docker.Api by embedding it (nil) and overriding
// only the calls Update reaches with run unset. It stores what ProjectUpdate
// sends, so ProjectGet reads back like DSM does.
type stubProjectAPI struct {
	docker.Api
	stored  string
	updates int
}

func (s *stubProjectAPI) ProjectGet(_ context.Context, id string) (*docker.Project, error) {
	return &docker.Project{ID: id, Content: s.stored, Status: "STOPPED"}, nil
}

func (s *stubProjectAPI) ProjectUpdate(
	_ context.Context,
	req docker.ProjectUpdateRequest,
) (*docker.ProjectUpdateResponse, error) {
	s.stored = req.Content
	s.updates++
	return &docker.ProjectUpdateResponse{}, nil
}

// TestUpdate_ContentDriftWithUnchangedServicesReachesDSM covers the review
// finding on PLAT-902: once content is planned from services, a content diff
// can arrive without a services diff (compose edited on DSM, or a networks,
// volumes, configs or secrets edit). Update gated ProjectUpdate on a services
// diff, so that content never reached DSM and the plan never converged.
func TestUpdate_ContentDriftWithUnchangedServicesReachesDSM(t *testing.T) {
	ctx := context.Background()

	plan := planWithServiceImage(t, ctx, "nginx:1")
	var planned models.ProjectResourceModel
	if diags := plan.Get(ctx, &planned); diags.HasError() {
		t.Fatalf("plan.Get() diagnostics: %s", diags)
	}
	var rendered string
	if diags := planned.ConfigRaw(ctx, &rendered); diags.HasError() {
		t.Fatalf("ConfigRaw() diagnostics: %s", diags)
	}
	planned.ID = types.StringValue("p1")
	planned.Content = types.StringValue(rendered)

	prior := planned
	prior.Content = types.StringValue("services:\n  app:\n    image: nginx:edited-on-dsm\n")

	if diags := plan.Set(ctx, &planned); diags.HasError() {
		t.Fatalf("plan.Set() diagnostics: %s", diags)
	}
	state := tfsdk.State{Schema: plan.Schema}
	if diags := state.Set(ctx, &prior); diags.HasError() {
		t.Fatalf("state.Set() diagnostics: %s", diags)
	}
	// Seeded from the prior state, as the framework server does.
	respState := tfsdk.State{Schema: plan.Schema}
	if diags := respState.Set(ctx, &prior); diags.HasError() {
		t.Fatalf("respState.Set() diagnostics: %s", diags)
	}

	api := &stubProjectAPI{stored: prior.Content.ValueString()}
	p := &ProjectResource{client: api}
	resp := resource.UpdateResponse{State: respState}
	p.Update(ctx, resource.UpdateRequest{Plan: plan, State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update() diagnostics: %s", resp.Diagnostics)
	}

	if api.stored != rendered {
		t.Errorf("DSM stores %q after Update, want planned content %q", api.stored, rendered)
	}
	var got models.ProjectResourceModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("resp.State.Get() diagnostics: %s", diags)
	}
	if got.Content.ValueString() != rendered {
		t.Errorf("state content = %q, want planned content %q", got.Content.ValueString(), rendered)
	}
}
