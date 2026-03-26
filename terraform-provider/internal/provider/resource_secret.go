package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	bellabaxter "github.com/cosmic-chimps/bella-baxter-go/bellabaxter"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure implementation satisfies the resource.Resource interface.
var _ resource.Resource = &secretResource{}
var _ resource.ResourceWithImportState = &secretResource{}

// NewSecretResource returns a factory for the bella_secret resource.
func NewSecretResource() resource.Resource {
	return &secretResource{}
}

// secretResource manages a single secret in Bella Baxter.
type secretResource struct {
	pc *providerClient
}

// secretResourceModel mirrors the HCL resource block schema.
type secretResourceModel struct {
	// Required inputs
	ProjectSlug     types.String `tfsdk:"project_slug"`
	EnvironmentSlug types.String `tfsdk:"environment_slug"`
	ProviderSlug    types.String `tfsdk:"provider_slug"`
	Key             types.String `tfsdk:"key"`
	Value           types.String `tfsdk:"value"`
	// Optional inputs
	Description types.String `tfsdk:"description"`
	// Composite resource ID
	ID types.String `tfsdk:"id"`
}

func (r *secretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *secretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a single secret in a Bella Baxter environment via the external provider (Vault, AWS, etc.).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite resource ID in the form `<project_slug>/<environment_slug>/<provider_slug>/<key>`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"project_slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Slug of the Bella Baxter project (e.g. `my-project`). Resolved automatically from the API key when omitted.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"environment_slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Slug of the Bella Baxter environment (e.g. `production`). Resolved automatically from the API key when omitted.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"provider_slug": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Slug of the provider (secret backend) within the environment (e.g. `my-vault`).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"key": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the secret (e.g. `RDS_PASSWORD`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				Required:            true,
				Sensitive:           true,
				MarkdownDescription: "The secret value. Always marked sensitive.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable description stored alongside the secret.",
			},
		},
	}
}

func (r *secretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pc, ok := req.ProviderData.(*providerClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *providerClient, got %T", req.ProviderData),
		)
		return
	}
	r.pc = pc
}

// Create creates the secret via the Bella Baxter API.
func (r *secretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectSlug := resolveSlug(plan.ProjectSlug, r.pc.defaultProjectSlug, "project_slug", &resp.Diagnostics)
	envSlug := resolveSlug(plan.EnvironmentSlug, r.pc.defaultEnvSlug, "environment_slug", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ProjectSlug = types.StringValue(projectSlug)
	plan.EnvironmentSlug = types.StringValue(envSlug)
	providerSlug := plan.ProviderSlug.ValueString()
	key := plan.Key.ValueString()
	value := plan.Value.ValueString()
	description := plan.Description.ValueString()

	if err := r.pc.client.CreateSecret(ctx, projectSlug, envSlug, providerSlug, key, value, description); err != nil {
		resp.Diagnostics.AddError("Failed to create secret", err.Error())
		return
	}

	plan.ID = types.StringValue(composeID(projectSlug, envSlug, providerSlug, key))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read verifies the secret still exists in the environment. If it has been
// deleted outside Terraform, the resource is removed from state so it will be
// recreated on the next apply.
func (r *secretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	secrets, err := r.pc.client.GetAllSecrets(ctx, state.ProjectSlug.ValueString(), state.EnvironmentSlug.ValueString())
	if err != nil {
		if bellabaxter.IsNotFoundError(err) {
			// Environment/project gone or API key no longer has access — remove from state.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read secrets", err.Error())
		return
	}

	key := state.Key.ValueString()
	if _, exists := secrets.Secrets[key]; !exists {
		// Secret deleted outside Terraform — remove from state so Terraform recreates it.
		resp.State.RemoveResource(ctx)
		return
	}

	// Note: we intentionally do NOT update state.Value from the API response.
	// Secret values are write-only from Terraform's perspective; reading them
	// back would expose them in plain-text state. We only verify existence.

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update updates the secret value and/or description.
func (r *secretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Carry the computed fields forward from state.
	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectSlug := resolveSlug(plan.ProjectSlug, r.pc.defaultProjectSlug, "project_slug", &resp.Diagnostics)
	envSlug := resolveSlug(plan.EnvironmentSlug, r.pc.defaultEnvSlug, "environment_slug", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ProjectSlug = types.StringValue(projectSlug)
	plan.EnvironmentSlug = types.StringValue(envSlug)
	providerSlug := plan.ProviderSlug.ValueString()
	key := plan.Key.ValueString()
	value := plan.Value.ValueString()
	description := plan.Description.ValueString()

	if err := r.pc.client.UpdateSecret(ctx, projectSlug, envSlug, providerSlug, key, value, description); err != nil {
		resp.Diagnostics.AddError("Failed to update secret", err.Error())
		return
	}

	// Preserve computed fields from existing state.
	plan.ID = state.ID

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes the secret from the external provider.
func (r *secretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.pc.client.DeleteSecret(
		ctx,
		state.ProjectSlug.ValueString(),
		state.EnvironmentSlug.ValueString(),
		state.ProviderSlug.ValueString(),
		state.Key.ValueString(),
	)
	if err != nil {
		var nfe *bellabaxter.NotFoundError
		if errors.As(err, &nfe) {
			// Already gone — treat as success.
			return
		}
		resp.Diagnostics.AddError("Failed to delete secret", err.Error())
	}
}

// ImportState supports `terraform import bella_secret.x <projectSlug>/<envSlug>/<providerSlug>/<key>`.
func (r *secretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 4)
	if len(parts) != 4 || parts[0] == "" || parts[1] == "" || parts[2] == "" || parts[3] == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"Import ID must be in the form <project_slug>/<environment_slug>/<provider_slug>/<key>.",
		)
		return
	}

	projectSlug, envSlug, providerSlug, key := parts[0], parts[1], parts[2], parts[3]

	state := secretResourceModel{
		ID:              types.StringValue(composeID(projectSlug, envSlug, providerSlug, key)),
		ProjectSlug:     types.StringValue(projectSlug),
		EnvironmentSlug: types.StringValue(envSlug),
		ProviderSlug:    types.StringValue(providerSlug),
		Key:             types.StringValue(key),
		Value:           types.StringValue(""), // unknown after import — will drift on next plan
		Description:     types.StringValue(""),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func composeID(projectSlug, envSlug, providerSlug, key string) string {
	return projectSlug + "/" + envSlug + "/" + providerSlug + "/" + key
}
