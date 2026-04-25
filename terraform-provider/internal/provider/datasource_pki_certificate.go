package provider

import (
	"context"
	"fmt"
	"strings"

	bellabaxter "github.com/cosmic-chimps/bella-baxter-go/bellabaxter"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &pkiCertificateDataSource{}

// NewPkiCertificateDataSource returns a factory for the bella_pki_certificate data source.
func NewPkiCertificateDataSource() datasource.DataSource {
	return &pkiCertificateDataSource{}
}

type pkiCertificateDataSource struct {
	pc *providerClient
}

type pkiCertificateDataSourceModel struct {
	// Inputs
	ProjectSlug     types.String `tfsdk:"project_slug"`
	EnvironmentSlug types.String `tfsdk:"environment_slug"`
	RoleName        types.String `tfsdk:"role_name"`
	CommonName      types.String `tfsdk:"common_name"`
	AltNames        types.String `tfsdk:"alt_names"`
	IpSans          types.String `tfsdk:"ip_sans"`
	Ttl             types.String `tfsdk:"ttl"`
	// Computed
	ID             types.String `tfsdk:"id"`
	Certificate    types.String `tfsdk:"certificate"`
	PrivateKey     types.String `tfsdk:"private_key"`
	PrivateKeyType types.String `tfsdk:"private_key_type"`
	IssuingCa      types.String `tfsdk:"issuing_ca"`
	CaChain        types.String `tfsdk:"ca_chain"`
	SerialNumber   types.String `tfsdk:"serial_number"`
	Expiration     types.Int64  `tfsdk:"expiration"`
}

func (d *pkiCertificateDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pki_certificate"
}

func (d *pkiCertificateDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Issues a short-lived X.509 TLS certificate from Bella Baxter's PKI engine. " +
			"The certificate and private key are available as outputs for use in provisioners or " +
			"file resources. The private key is marked sensitive and never stored in plain text in " +
			"state beyond what Terraform itself manages.\n\n" +
			"> **Note:** A new certificate is issued on every `terraform apply`. " +
			"For automatic renewal without Terraform, use an ACME client pointed at the " +
			"`acme_directory_url` from the `bella_pki_ca` data source.",
		Attributes: map[string]schema.Attribute{
			"project_slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Slug of the Bella Baxter project. Resolved automatically from the API key when omitted.",
			},
			"environment_slug": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Slug of the Bella Baxter environment. Resolved automatically from the API key when omitted.",
			},
			"role_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "PKI role that governs which Common Names and TTLs are allowed.",
			},
			"common_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Common Name (CN) for the certificate (e.g. `api.internal.example.com`). Must match the role's `allowed_domains`.",
			},
			"alt_names": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Comma-separated Subject Alternative Names (SANs), e.g. `api.staging.internal,api-v2.staging.internal`.",
			},
			"ip_sans": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Comma-separated IP SANs (e.g. `10.0.0.5,192.168.1.1`).",
			},
			"ttl": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Certificate lifetime (e.g. `24h`). Defaults to the role's `default_ttl`.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Certificate serial number.",
			},
			"certificate": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The issued certificate in PEM format.",
			},
			"private_key": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "The private key in PEM format. Marked sensitive — save it immediately, it is only available at issuance time.",
			},
			"private_key_type": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Key algorithm used (e.g. `rsa`).",
			},
			"issuing_ca": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The issuing CA certificate in PEM format.",
			},
			"ca_chain": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Full CA chain in PEM format (newline-joined).",
			},
			"serial_number": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Certificate serial number (colon-delimited hex).",
			},
			"expiration": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Unix timestamp (seconds) when the certificate expires.",
			},
		},
	}
}

func (d *pkiCertificateDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *pkiCertificateDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state pkiCertificateDataSourceModel
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

	issueReq := bellabaxter.PkiIssueCertificateRequest{
		RoleName:   state.RoleName.ValueString(),
		CommonName: state.CommonName.ValueString(),
	}
	if !state.AltNames.IsNull() && !state.AltNames.IsUnknown() {
		v := state.AltNames.ValueString()
		issueReq.AltNames = &v
	}
	if !state.IpSans.IsNull() && !state.IpSans.IsUnknown() {
		v := state.IpSans.ValueString()
		issueReq.IpSans = &v
	}
	if !state.Ttl.IsNull() && !state.Ttl.IsUnknown() {
		v := state.Ttl.ValueString()
		issueReq.Ttl = &v
	}

	result, err := d.pc.client.IssuePkiCertificate(ctx, projectSlug, envSlug, issueReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to issue PKI certificate", err.Error())
		return
	}

	state.ID = types.StringValue(result.SerialNumber)
	state.Certificate = types.StringValue(result.Certificate)
	state.PrivateKey = types.StringValue(result.PrivateKey)
	state.PrivateKeyType = types.StringValue(result.PrivateKeyType)
	state.IssuingCa = types.StringValue(result.IssuingCa)
	state.CaChain = types.StringValue(strings.Join(result.CaChain, "\n"))
	state.SerialNumber = types.StringValue(result.SerialNumber)
	state.Expiration = types.Int64Value(result.Expiration)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
