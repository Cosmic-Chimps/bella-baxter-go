package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure implementation satisfies the datasource.DataSource interface.
var _ datasource.DataSource = &secretDataSource{}

// NewSecretDataSource returns a factory for the bella_secret data source.
func NewSecretDataSource() datasource.DataSource {
	return &secretDataSource{}
}

// secretDataSource reads a single secret value from Bella Baxter.
type secretDataSource struct {
	pc *providerClient
}

// secretDataSourceModel mirrors the HCL data block schema.
type secretDataSourceModel struct {
	// Inputs
	Key             types.String `tfsdk:"key"`
	ProjectSlug     types.String `tfsdk:"project_slug"`
	EnvironmentSlug types.String `tfsdk:"environment_slug"`
	// Computed
	ID    types.String `tfsdk:"id"`
	Value types.String `tfsdk:"value"`
}

func (d *secretDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (d *secretDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read a single secret from Bella Baxter.",
		Attributes: map[string]schema.Attribute{
			"key": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The name of the secret to retrieve (e.g. `RDS_PASSWORD`).",
			},
			"project_slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The project slug (e.g. `my-project`). Resolved automatically from the API key when omitted.",
			},
			"environment_slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "The environment slug (e.g. `production`). Resolved automatically from the API key when omitted.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique identifier in the form `<environment_slug>/<key>`.",
			},
			"value": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "The secret value. Marked sensitive so Terraform hides it from output.",
			},
		},
	}
}

func (d *secretDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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
	d.pc = pc
}

func (d *secretDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state secretDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectSlug := resolveSlug(state.ProjectSlug, d.pc.defaultProjectSlug, "project_slug", &resp.Diagnostics)
	envSlug := resolveSlug(state.EnvironmentSlug, d.pc.defaultEnvSlug, "environment_slug", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	secrets, err := d.pc.client.GetAllSecrets(ctx, projectSlug, envSlug)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read secrets", err.Error())
		return
	}

	key := state.Key.ValueString()
	value, ok := secrets.Secrets[key]
	if !ok {
		resp.Diagnostics.AddError(
			"Secret not found",
			fmt.Sprintf("Key %q does not exist in environment %q.", key, envSlug),
		)
		return
	}

	state.ID = types.StringValue(projectSlug + "/" + envSlug + "/" + key)
	state.ProjectSlug = types.StringValue(projectSlug)
	state.EnvironmentSlug = types.StringValue(envSlug)
	state.Value = types.StringValue(value)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
