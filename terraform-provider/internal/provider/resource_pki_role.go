package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	bellabaxter "github.com/cosmic-chimps/bella-baxter-go/bellabaxter"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &pkiRoleResource{}
var _ resource.ResourceWithImportState = &pkiRoleResource{}

// NewPkiRoleResource returns a factory for the bella_pki_role resource.
func NewPkiRoleResource() resource.Resource {
	return &pkiRoleResource{}
}

type pkiRoleResource struct {
	pc *providerClient
}

type pkiRoleResourceModel struct {
	// Required inputs
	ProjectSlug     types.String `tfsdk:"project_slug"`
	EnvironmentSlug types.String `tfsdk:"environment_slug"`
	Name            types.String `tfsdk:"name"`
	AllowedDomains  types.String `tfsdk:"allowed_domains"`
	// Optional inputs
	AllowSubdomains types.Bool   `tfsdk:"allow_subdomains"`
	AllowLocalhost  types.Bool   `tfsdk:"allow_localhost"`
	AllowAnyName    types.Bool   `tfsdk:"allow_any_name"`
	DefaultTtl      types.String `tfsdk:"default_ttl"`
	MaxTtl          types.String `tfsdk:"max_ttl"`
	KeyType         types.String `tfsdk:"key_type"`
	// Composite resource ID
	ID types.String `tfsdk:"id"`
}

func (r *pkiRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pki_role"
}

func (r *pkiRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a PKI role in a Bella Baxter environment. " +
			"PKI roles define which domains may be used as Common Names, " +
			"the maximum certificate TTL, and the key type for issued certificates.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Composite resource ID in the form `<project>/<env>/<role_name>`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"project_slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Slug of the Bella Baxter project. Resolved automatically from the API key when omitted.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"environment_slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Slug of the Bella Baxter environment. Resolved automatically from the API key when omitted.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the PKI role (e.g. `internal-service`). Changing this forces a new resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"allowed_domains": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Base domain(s) certificates may be issued for (e.g. `internal.example.com`). Do NOT include wildcards — use `allow_subdomains = true` instead.",
			},
			"allow_subdomains": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Allow certificates for subdomains of `allowed_domains`. Defaults to `true`.",
			},
			"allow_localhost": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Allow `localhost` as a Common Name or SAN. Defaults to `false`.",
			},
			"allow_any_name": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Allow any Common Name regardless of `allowed_domains`. Defaults to `false`.",
			},
			"default_ttl": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Default certificate TTL (e.g. `720h`). Defaults to `720h`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"max_ttl": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Maximum certificate TTL (e.g. `8760h`). Defaults to `8760h`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"key_type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Key algorithm for issued certificates: `rsa` or `ec`. Defaults to `rsa`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *pkiRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pc, ok := req.ProviderData.(*providerClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *providerClient, got %T", req.ProviderData))
		return
	}
	r.pc = pc
}

func (r *pkiRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan pkiRoleResourceModel
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

	defaultTtl := plan.DefaultTtl.ValueString()
	if defaultTtl == "" {
		defaultTtl = "720h"
	}
	maxTtl := plan.MaxTtl.ValueString()
	if maxTtl == "" {
		maxTtl = "8760h"
	}
	keyType := plan.KeyType.ValueString()
	if keyType == "" {
		keyType = "rsa"
	}

	if err := r.pc.client.CreatePkiRole(ctx, projectSlug, envSlug, bellabaxter.PkiCreateRoleRequest{
		Name:            plan.Name.ValueString(),
		AllowedDomains:  plan.AllowedDomains.ValueString(),
		AllowSubdomains: plan.AllowSubdomains.ValueBool(),
		AllowLocalhost:  plan.AllowLocalhost.ValueBool(),
		AllowAnyName:    plan.AllowAnyName.ValueBool(),
		DefaultTtl:      defaultTtl,
		MaxTtl:          maxTtl,
		KeyType:         keyType,
	}); err != nil {
		resp.Diagnostics.AddError("Failed to create PKI role", err.Error())
		return
	}

	plan.ID = types.StringValue(composePkiID(projectSlug, envSlug, plan.Name.ValueString()))
	plan.DefaultTtl = types.StringValue(defaultTtl)
	plan.MaxTtl = types.StringValue(maxTtl)
	plan.KeyType = types.StringValue(keyType)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *pkiRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state pkiRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectSlug := state.ProjectSlug.ValueString()
	envSlug := state.EnvironmentSlug.ValueString()
	name := state.Name.ValueString()

	rolesResp, err := r.pc.client.ListPkiRoles(ctx, projectSlug, envSlug)
	if err != nil {
		var nfe *bellabaxter.NotFoundError
		if errors.As(err, &nfe) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read PKI roles", err.Error())
		return
	}

	for _, role := range rolesResp.Roles {
		if role.Name == name {
			state.AllowedDomains = types.StringValue(role.AllowedDomains)
			state.AllowSubdomains = types.BoolValue(role.AllowSubdomains)
			state.AllowLocalhost = types.BoolValue(role.AllowLocalhost)
			state.AllowAnyName = types.BoolValue(role.AllowAnyName)
			state.DefaultTtl = types.StringValue(role.DefaultTtl)
			state.MaxTtl = types.StringValue(role.MaxTtl)
			state.KeyType = types.StringValue(role.KeyType)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	// Role not found — remove from state so Terraform recreates it.
	resp.State.RemoveResource(ctx)
}

// Update recreates the PKI role (delete + create) since there is no PATCH endpoint.
func (r *pkiRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan pkiRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state pkiRoleResourceModel
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

	if err := r.pc.client.DeletePkiRole(ctx, projectSlug, envSlug, name); err != nil {
		var nfe *bellabaxter.NotFoundError
		if !errors.As(err, &nfe) {
			resp.Diagnostics.AddError("Failed to delete PKI role during update", err.Error())
			return
		}
	}

	defaultTtl := plan.DefaultTtl.ValueString()
	if defaultTtl == "" {
		defaultTtl = "720h"
	}
	maxTtl := plan.MaxTtl.ValueString()
	if maxTtl == "" {
		maxTtl = "8760h"
	}
	keyType := plan.KeyType.ValueString()
	if keyType == "" {
		keyType = "rsa"
	}

	if err := r.pc.client.CreatePkiRole(ctx, projectSlug, envSlug, bellabaxter.PkiCreateRoleRequest{
		Name:            name,
		AllowedDomains:  plan.AllowedDomains.ValueString(),
		AllowSubdomains: plan.AllowSubdomains.ValueBool(),
		AllowLocalhost:  plan.AllowLocalhost.ValueBool(),
		AllowAnyName:    plan.AllowAnyName.ValueBool(),
		DefaultTtl:      defaultTtl,
		MaxTtl:          maxTtl,
		KeyType:         keyType,
	}); err != nil {
		resp.Diagnostics.AddError("Failed to recreate PKI role during update", err.Error())
		return
	}

	plan.ID = state.ID
	plan.DefaultTtl = types.StringValue(defaultTtl)
	plan.MaxTtl = types.StringValue(maxTtl)
	plan.KeyType = types.StringValue(keyType)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *pkiRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state pkiRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.pc.client.DeletePkiRole(ctx, state.ProjectSlug.ValueString(), state.EnvironmentSlug.ValueString(), state.Name.ValueString())
	if err != nil {
		var nfe *bellabaxter.NotFoundError
		if errors.As(err, &nfe) {
			return
		}
		resp.Diagnostics.AddError("Failed to delete PKI role", err.Error())
	}
}

// ImportState supports `terraform import bella_pki_role.x <project>/<env>/<name>`.
func (r *pkiRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Import ID must be in the form <project_slug>/<environment_slug>/<name>.")
		return
	}

	projectSlug, envSlug, name := parts[0], parts[1], parts[2]
	state := pkiRoleResourceModel{
		ID:              types.StringValue(composePkiID(projectSlug, envSlug, name)),
		ProjectSlug:     types.StringValue(projectSlug),
		EnvironmentSlug: types.StringValue(envSlug),
		Name:            types.StringValue(name),
		AllowedDomains:  types.StringValue(""),
		AllowSubdomains: types.BoolValue(true),
		AllowLocalhost:  types.BoolValue(false),
		AllowAnyName:    types.BoolValue(false),
		DefaultTtl:      types.StringValue(""),
		MaxTtl:          types.StringValue(""),
		KeyType:         types.StringValue(""),
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func composePkiID(projectSlug, envSlug, name string) string {
	return projectSlug + "/" + envSlug + "/" + name
}
