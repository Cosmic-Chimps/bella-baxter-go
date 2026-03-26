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
var _ resource.Resource = &sshRoleResource{}
var _ resource.ResourceWithImportState = &sshRoleResource{}

// NewSshRoleResource returns a factory for the bella_ssh_role resource.
func NewSshRoleResource() resource.Resource {
	return &sshRoleResource{}
}

// sshRoleResource manages a single SSH role in Bella Baxter.
type sshRoleResource struct {
	pc *providerClient
}

// sshRoleResourceModel mirrors the HCL resource block schema.
type sshRoleResourceModel struct {
	// Required inputs
	ProjectSlug     types.String `tfsdk:"project_slug"`
	EnvironmentSlug types.String `tfsdk:"environment_slug"`
	Name            types.String `tfsdk:"name"`
	AllowedUsers    types.String `tfsdk:"allowed_users"`
	// Optional inputs
	DefaultTtl types.String `tfsdk:"default_ttl"`
	MaxTtl     types.String `tfsdk:"max_ttl"`
	// Composite resource ID
	ID types.String `tfsdk:"id"`
}

func (r *sshRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_role"
}

func (r *sshRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages an SSH role in a Bella Baxter environment. SSH roles define who can receive signed certificates and for how long.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite resource ID in the form `<project>/<env>/<role_name>`.",
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
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the SSH role (e.g. `ops-team`). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"allowed_users": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Comma-separated list of allowed Unix usernames (e.g. `ec2-user,ubuntu,admin`).",
			},
			"default_ttl": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Default certificate TTL (e.g. `8h`). Defaults to `8h`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"max_ttl": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Maximum certificate TTL (e.g. `24h`). Defaults to `24h`.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *sshRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create creates the SSH role via the Bella Baxter API.
func (r *sshRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshRoleResourceModel
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
	name := plan.Name.ValueString()
	allowedUsers := plan.AllowedUsers.ValueString()
	defaultTtl := plan.DefaultTtl.ValueString()
	if defaultTtl == "" {
		defaultTtl = "8h"
	}
	maxTtl := plan.MaxTtl.ValueString()
	if maxTtl == "" {
		maxTtl = "24h"
	}

	if err := r.pc.client.CreateSshRole(ctx, projectSlug, envSlug, bellabaxter.SshCreateRoleRequest{
		Name:         name,
		AllowedUsers: allowedUsers,
		DefaultTtl:   defaultTtl,
		MaxTtl:       maxTtl,
	}); err != nil {
		resp.Diagnostics.AddError("Failed to create SSH role", err.Error())
		return
	}

	plan.ID = types.StringValue(composeSshID(projectSlug, envSlug, name))
	plan.DefaultTtl = types.StringValue(defaultTtl)
	plan.MaxTtl = types.StringValue(maxTtl)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read verifies the SSH role still exists. Removes from state if gone.
func (r *sshRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sshRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	envSlug := state.EnvironmentSlug.ValueString()
	projectSlug := state.ProjectSlug.ValueString()
	name := state.Name.ValueString()

	rolesResp, err := r.pc.client.ListSshRoles(ctx, projectSlug, envSlug)
	if err != nil {
		var nfe *bellabaxter.NotFoundError
		if errors.As(err, &nfe) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read SSH roles", err.Error())
		return
	}

	for _, role := range rolesResp.Roles {
		if role.Name == name {
			state.AllowedUsers = types.StringValue(role.AllowedUsers)
			state.DefaultTtl = types.StringValue(role.DefaultTtl)
			state.MaxTtl = types.StringValue(role.MaxTtl)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	// Role not found — remove from state so Terraform recreates it.
	resp.State.RemoveResource(ctx)
}

// Update recreates the SSH role (delete + create) since there is no PATCH endpoint.
func (r *sshRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sshRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state sshRoleResourceModel
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
	name := plan.Name.ValueString()

	// Delete existing role first.
	if err := r.pc.client.DeleteSshRole(ctx, projectSlug, envSlug, name); err != nil {
		var nfe *bellabaxter.NotFoundError
		if !errors.As(err, &nfe) {
			resp.Diagnostics.AddError("Failed to delete SSH role during update", err.Error())
			return
		}
	}

	defaultTtl := plan.DefaultTtl.ValueString()
	if defaultTtl == "" {
		defaultTtl = "8h"
	}
	maxTtl := plan.MaxTtl.ValueString()
	if maxTtl == "" {
		maxTtl = "24h"
	}

	// Recreate with new settings.
	if err := r.pc.client.CreateSshRole(ctx, projectSlug, envSlug, bellabaxter.SshCreateRoleRequest{
		Name:         name,
		AllowedUsers: plan.AllowedUsers.ValueString(),
		DefaultTtl:   defaultTtl,
		MaxTtl:       maxTtl,
	}); err != nil {
		resp.Diagnostics.AddError("Failed to recreate SSH role during update", err.Error())
		return
	}

	plan.ID = state.ID
	plan.DefaultTtl = types.StringValue(defaultTtl)
	plan.MaxTtl = types.StringValue(maxTtl)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes the SSH role from the provider.
func (r *sshRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sshRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.pc.client.DeleteSshRole(
		ctx,
		state.ProjectSlug.ValueString(),
		state.EnvironmentSlug.ValueString(),
		state.Name.ValueString(),
	)
	if err != nil {
		var nfe *bellabaxter.NotFoundError
		if errors.As(err, &nfe) {
			return // Already gone — treat as success.
		}
		resp.Diagnostics.AddError("Failed to delete SSH role", err.Error())
	}
}

// ImportState supports `terraform import bella_ssh_role.x <project>/<env>/<name>`.
func (r *sshRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			"Import ID must be in the form <project_slug>/<environment_slug>/<name>.",
		)
		return
	}

	projectSlug, envSlug, name := parts[0], parts[1], parts[2]

	state := sshRoleResourceModel{
		ID:              types.StringValue(composeSshID(projectSlug, envSlug, name)),
		ProjectSlug:     types.StringValue(projectSlug),
		EnvironmentSlug: types.StringValue(envSlug),
		Name:            types.StringValue(name),
		AllowedUsers:    types.StringValue(""),
		DefaultTtl:      types.StringValue(""),
		MaxTtl:          types.StringValue(""),
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func composeSshID(projectSlug, envSlug, name string) string {
	return projectSlug + "/" + envSlug + "/" + name
}
