package provider

import (
	"context"
	"fmt"

	bellabaxter "github.com/cosmic-chimps/bella-baxter-go/bellabaxter"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure implementation satisfies the datasource.DataSource interface.
var _ datasource.DataSource = &sshSignedCertDataSource{}

// NewSshSignedCertDataSource returns a factory for the bella_ssh_signed_certificate data source.
func NewSshSignedCertDataSource() datasource.DataSource {
	return &sshSignedCertDataSource{}
}

// sshSignedCertDataSource signs a public key via Bella Baxter and returns the certificate.
type sshSignedCertDataSource struct {
	pc *providerClient
}

// sshSignedCertDataSourceModel mirrors the HCL data block schema.
type sshSignedCertDataSourceModel struct {
	// Inputs
	ProjectSlug     types.String `tfsdk:"project_slug"`
	EnvironmentSlug  types.String `tfsdk:"environment_slug"`
	RoleName         types.String `tfsdk:"role_name"`
	PublicKey        types.String `tfsdk:"public_key"`
	ValidPrincipals  types.String `tfsdk:"valid_principals"`
	Ttl              types.String `tfsdk:"ttl"`
	// Computed
	ID          types.String `tfsdk:"id"`
	SignedKey   types.String `tfsdk:"signed_key"`
	Serial      types.String `tfsdk:"serial"`
	ExpiresAt   types.String `tfsdk:"expires_at"`
}

func (d *sshSignedCertDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_signed_certificate"
}

func (d *sshSignedCertDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Signs an SSH public key via a Bella Baxter environment and returns a short-lived certificate. Use the `signed_key` output as a Terraform `connection.certificate` to connect to CA-trusted hosts without static key pairs.",
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
			"role_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "SSH role that controls which principals and TTLs are allowed.",
			},
			"public_key": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "OpenSSH public key to sign (e.g. contents of `~/.ssh/id_ed25519.pub`).",
			},
			"valid_principals": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Comma-separated Unix usernames the certificate will be valid for (e.g. `ubuntu,ec2-user`). Defaults to the role's allowed_users.",
			},
			"ttl": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Certificate lifetime (e.g. `1h`). Defaults to the role's default_ttl.",
			},
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Certificate serial number.",
			},
			"signed_key": schema.StringAttribute{
				Computed:            true,
				Sensitive:           true,
				MarkdownDescription: "The signed SSH certificate. Pass this to `connection.certificate` in a Terraform resource.",
			},
			"serial": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Certificate serial number.",
			},
			"expires_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Certificate expiry timestamp (RFC3339).",
			},
		},
	}
}

func (d *sshSignedCertDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *sshSignedCertDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state sshSignedCertDataSourceModel
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

	signReq := bellabaxter.SshSignRequest{
		PublicKey: state.PublicKey.ValueString(),
		RoleName:  state.RoleName.ValueString(),
	}
	if !state.ValidPrincipals.IsNull() && !state.ValidPrincipals.IsUnknown() {
		v := state.ValidPrincipals.ValueString()
		signReq.ValidPrincipals = &v
	}
	if !state.Ttl.IsNull() && !state.Ttl.IsUnknown() {
		v := state.Ttl.ValueString()
		signReq.Ttl = &v
	}

	result, err := d.pc.client.SignSshKey(ctx, projectSlug, envSlug, signReq)
	if err != nil {
		resp.Diagnostics.AddError("Failed to sign SSH key", err.Error())
		return
	}

	state.ID = types.StringValue(result.SerialNumber)
	state.SignedKey = types.StringValue(result.SignedKey)
	state.Serial = types.StringValue(result.SerialNumber)
	state.ExpiresAt = types.StringValue("") // not returned by API currently

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
