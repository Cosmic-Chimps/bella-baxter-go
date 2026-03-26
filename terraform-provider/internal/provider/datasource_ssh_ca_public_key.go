package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure implementation satisfies the datasource.DataSource interface.
var _ datasource.DataSource = &sshCaPublicKeyDataSource{}

// NewSshCaPublicKeyDataSource returns a factory for the bella_ssh_ca_public_key data source.
func NewSshCaPublicKeyDataSource() datasource.DataSource {
	return &sshCaPublicKeyDataSource{}
}

// sshCaPublicKeyDataSource reads the SSH CA public key from Bella Baxter.
type sshCaPublicKeyDataSource struct {
	pc *providerClient
}

// sshCaPublicKeyDataSourceModel mirrors the HCL data block schema.
type sshCaPublicKeyDataSourceModel struct {
	// Inputs
	ProjectSlug     types.String `tfsdk:"project_slug"`
	EnvironmentSlug types.String `tfsdk:"environment_slug"`
	// Computed
	ID               types.String `tfsdk:"id"`
	CaPublicKey      types.String `tfsdk:"ca_public_key"`
	Instructions     types.String `tfsdk:"instructions"`
	TerraformSnippet types.String `tfsdk:"terraform_snippet"`
}

func (d *sshCaPublicKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_ca_public_key"
}

func (d *sshCaPublicKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the SSH CA public key from a Bella Baxter environment. Use this to configure `TrustedUserCAKeys` on target hosts.",
		Attributes: map[string]schema.Attribute{
			"project_slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Slug of the Bella Baxter project (e.g. `my-project`). Resolved automatically from the API key when omitted.",
			},
			"environment_slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Slug of the Bella Baxter environment (e.g. `production`). Resolved automatically from the API key when omitted.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Unique identifier in the form `<project_slug>/<environment_slug>`.",
			},
			"ca_public_key": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The OpenSSH CA public key. Add this as a `TrustedUserCAKeys` entry on your SSH servers.",
			},
			"instructions": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable instructions for trusting this CA.",
			},
			"terraform_snippet": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "A ready-to-use Terraform snippet for writing the CA key to target hosts.",
			},
		},
	}
}

func (d *sshCaPublicKeyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *sshCaPublicKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state sshCaPublicKeyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	envSlug := resolveSlug(state.EnvironmentSlug, d.pc.defaultEnvSlug, "environment_slug", &resp.Diagnostics)
	projectSlug := resolveSlug(state.ProjectSlug, d.pc.defaultProjectSlug, "project_slug", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ProjectSlug = types.StringValue(projectSlug)
	state.EnvironmentSlug = types.StringValue(envSlug)

	result, err := d.pc.client.GetSshCaPublicKey(ctx, projectSlug, envSlug)
	if err != nil {
		resp.Diagnostics.AddError("Failed to fetch SSH CA public key", err.Error())
		return
	}

	state.ID = types.StringValue(projectSlug + "/" + envSlug)
	state.CaPublicKey = types.StringValue(result.CaPublicKey)
	state.Instructions = types.StringValue(result.Instructions)
	state.TerraformSnippet = types.StringValue(result.TerraformSnippet)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
