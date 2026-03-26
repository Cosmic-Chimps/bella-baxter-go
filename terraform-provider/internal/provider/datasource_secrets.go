package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure implementation satisfies the datasource.DataSource interface.
var _ datasource.DataSource = &secretsDataSource{}

// NewSecretsDataSource returns a factory for the bella_secrets data source.
func NewSecretsDataSource() datasource.DataSource {
	return &secretsDataSource{}
}

// secretsDataSource reads all secrets for an environment from Bella Baxter.
type secretsDataSource struct {
	pc *providerClient
}

// secretsDataSourceModel mirrors the HCL data block schema.
type secretsDataSourceModel struct {
	// Inputs
	ProjectSlug     types.String `tfsdk:"project_slug"`
	EnvironmentSlug types.String `tfsdk:"environment_slug"`
	// Computed
	ID      types.String `tfsdk:"id"`
	Secrets types.Map    `tfsdk:"secrets"`
	Version types.Int64  `tfsdk:"version"`
}

func (d *secretsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secrets"
}

func (d *secretsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Read all secrets for an environment from Bella Baxter.",
		Attributes: map[string]schema.Attribute{
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
				MarkdownDescription: "Unique identifier for this data source (the environment slug).",
			},
			"secrets": schema.MapAttribute{
				Computed:            true,
				Sensitive:           true,
				ElementType:         types.StringType,
				MarkdownDescription: "Map of all secret key→value pairs. Marked sensitive so Terraform hides values from output.",
			},
			"version": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Monotonically increasing version counter for the environment's secrets.",
			},
		},
	}
}

func (d *secretsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *secretsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state secretsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectSlug := resolveSlug(state.ProjectSlug, d.pc.defaultProjectSlug, "project_slug", &resp.Diagnostics)
	envSlug := resolveSlug(state.EnvironmentSlug, d.pc.defaultEnvSlug, "environment_slug", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := d.pc.client.GetAllSecrets(ctx, projectSlug, envSlug)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read secrets", err.Error())
		return
	}

	// Convert map[string]string → types.Map
	elems := make(map[string]attr.Value, len(result.Secrets))
	for k, v := range result.Secrets {
		elems[k] = types.StringValue(v)
	}
	secretsMap, diags := types.MapValue(types.StringType, elems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.ID = types.StringValue(projectSlug + "/" + envSlug)
	state.ProjectSlug = types.StringValue(projectSlug)
	state.EnvironmentSlug = types.StringValue(envSlug)
	state.Secrets = secretsMap
	state.Version = types.Int64Value(result.Version)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
