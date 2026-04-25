package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &pkiCaDataSource{}

// NewPkiCaDataSource returns a factory for the bella_pki_ca data source.
func NewPkiCaDataSource() datasource.DataSource {
	return &pkiCaDataSource{}
}

type pkiCaDataSource struct {
	pc *providerClient
}

type pkiCaDataSourceModel struct {
	// Inputs
	ProjectSlug     types.String `tfsdk:"project_slug"`
	EnvironmentSlug types.String `tfsdk:"environment_slug"`
	// Computed
	ID               types.String `tfsdk:"id"`
	Certificate      types.String `tfsdk:"certificate"`
	CaChain          types.String `tfsdk:"ca_chain"`
	Instructions     types.String `tfsdk:"instructions"`
	AcmeDirectoryUrl types.String `tfsdk:"acme_directory_url"`
}

func (d *pkiCaDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pki_ca"
}

func (d *pkiCaDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Reads the PKI CA certificate from a Bella Baxter environment. " +
			"Distribute the `certificate` to the trust stores of all machines that need to trust " +
			"TLS certificates issued by this CA.",
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
			"certificate": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The CA certificate in PEM format. Write this to `/etc/ssl/certs/` or use it as a `ca_bundle` in cert-manager.",
			},
			"ca_chain": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full CA chain in PEM format (for intermediate CA setups).",
			},
			"instructions": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Human-readable instructions for distributing this CA certificate.",
			},
			"acme_directory_url": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "ACME directory URL (RFC 8555) for this CA. Pass to certbot, acme.sh, Caddy, or cert-manager for automatic certificate issuance and renewal.",
			},
		},
	}
}

func (d *pkiCaDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pc, ok := req.ProviderData.(*providerClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *providerClient, got %T", req.ProviderData))
		return
	}
	d.pc = pc
}

func (d *pkiCaDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state pkiCaDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	projectSlug := resolveSlug(state.ProjectSlug, d.pc.defaultProjectSlug, "project_slug", &resp.Diagnostics)
	envSlug := resolveSlug(state.EnvironmentSlug, d.pc.defaultEnvSlug, "environment_slug", &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ProjectSlug = types.StringValue(projectSlug)
	state.EnvironmentSlug = types.StringValue(envSlug)

	result, err := d.pc.client.GetPkiCa(ctx, projectSlug, envSlug)
	if err != nil {
		resp.Diagnostics.AddError("Failed to fetch PKI CA", err.Error())
		return
	}

	state.ID = types.StringValue(projectSlug + "/" + envSlug)
	state.Certificate = types.StringValue(result.Certificate)
	state.CaChain = types.StringValue(result.CaChain)
	state.Instructions = types.StringValue(result.Instructions)
	if result.AcmeDirectoryUrl != nil {
		state.AcmeDirectoryUrl = types.StringValue(*result.AcmeDirectoryUrl)
	} else {
		state.AcmeDirectoryUrl = types.StringValue("")
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
