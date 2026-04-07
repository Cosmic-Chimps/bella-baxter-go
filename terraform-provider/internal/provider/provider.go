// Package provider implements the Bella Baxter Terraform provider.
package provider

import (
	"context"
	"os"

	bellabaxter "github.com/cosmic-chimps/bella-baxter-go/bellabaxter"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure bellaProvider satisfies the provider.Provider interface.
var _ provider.Provider = &bellaProvider{}
var _ provider.ProviderWithFunctions = &bellaProvider{}

// bellaProvider is the root provider implementation.
type bellaProvider struct {
	version string
}

// bellaProviderModel mirrors the HCL provider block schema.
type bellaProviderModel struct {
	BaxterURL  types.String `tfsdk:"baxter_url"`
	ApiKey     types.String `tfsdk:"api_key"`
	AppName    types.String `tfsdk:"app_name"`
	PrivateKey types.String `tfsdk:"private_key"`
}

// New returns a provider factory that creates bellaProvider instances.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &bellaProvider{version: version}
	}
}

func (p *bellaProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "bella"
	resp.Version = p.version
}

func (p *bellaProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Interact with [Bella Baxter](https://github.com/cosmic-chimps/bella-baxter) — a secret management gateway.",
		Attributes: map[string]schema.Attribute{
			"baxter_url": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Base URL of the Bella Baxter API (e.g. `https://baxter.example.com`). Can also be set via the `BELLA_BAXTER_URL` environment variable.",
			},
			"api_key": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Bella Baxter API key (starts with `bax-`). The key already encodes which project and environment it is scoped to — `project_slug` and `environment_slug` are resolved automatically via `GET /api/v1/keys/me`. Can also be set via the `BELLA_API_KEY` environment variable.",
			},
			"app_name": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Name of your application, sent as the `X-App-Client` header for audit logging (e.g. `my-infra`, `payment-service`). Can also be set via the `BELLA_BAXTER_APP_CLIENT` environment variable.",
			},
			"private_key": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "PKCS#8 PEM private key for Zero-Knowledge Encryption (ZKE). " +
					"When set, Terraform uses a persistent device key for transport encryption so that " +
					"audit logs can identify exactly which runner fetched secrets. " +
					"Generate with `bella auth setup`. " +
					"Can also be set via the `BELLA_BAXTER_PRIVATE_KEY` environment variable (recommended for CI).",
			},
		},
	}
}

// Configure initialises the shared client and stores it in resp.DataSourceData /
// resp.ResourceData so all resources and data sources can access it.
func (p *bellaProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config bellaProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	baxterURL := resolveString(config.BaxterURL, "BELLA_BAXTER_URL")
	apiKey := resolveString(config.ApiKey, "BELLA_API_KEY")
	appName := resolveString(config.AppName, "BELLA_BAXTER_APP_CLIENT")
	privateKey := resolveString(config.PrivateKey, "BELLA_BAXTER_PRIVATE_KEY")

	if baxterURL == "" {
		resp.Diagnostics.AddError(
			"Missing baxter_url",
			"Set baxter_url in the provider block or the BELLA_BAXTER_URL environment variable.",
		)
	}
	if apiKey == "" {
		resp.Diagnostics.AddError(
			"Missing api_key",
			"Set api_key in the provider block or the BELLA_API_KEY environment variable.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	client, err := bellabaxter.New(bellabaxter.Options{
		BaxterURL:    baxterURL,
		ApiKey:       apiKey,
		AppClient:    appName,
		PrivateKeyPEM: privateKey,
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Bella Baxter client", err.Error())
		return
	}

	// Discover project + environment from the API key metadata.
	// GET /api/v1/keys/me — the key is already scoped to a project + environment,
	// so callers never need to specify project_slug / environment_slug explicitly.
	keyCtx, err := client.GetKeyContext(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to resolve API key context",
			"Could not call GET /api/v1/keys/me: "+err.Error(),
		)
		return
	}

	pc := &providerClient{
		client:             client,
		defaultProjectSlug: keyCtx.ProjectSlug,
		defaultEnvSlug:     keyCtx.EnvironmentSlug,
	}
	resp.DataSourceData = pc
	resp.ResourceData = pc
}

func (p *bellaProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSecretResource,
		NewSshRoleResource,
	}
}

func (p *bellaProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewSecretDataSource,
		NewSecretsDataSource,
		NewSshCaPublicKeyDataSource,
		NewSshSignedCertDataSource,
	}
}

func (p *bellaProvider) Functions(_ context.Context) []func() function.Function {
	return nil
}

// ── Shared provider client wrapper ────────────────────────────────────────────

// providerClient is passed as DataSourceData / ResourceData to all resources.
type providerClient struct {
	client             *bellabaxter.Client
	defaultProjectSlug string
	defaultEnvSlug     string
}

// resolveString returns the value from a types.String if set, otherwise falls
// back to the named environment variable.
func resolveString(v types.String, envVar string) string {
	if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		return v.ValueString()
	}
	return os.Getenv(envVar)
}

// resolveSlug returns the explicit value if non-empty, otherwise the provider-level default.
// If both are empty it appends an error diagnostic.
func resolveSlug(explicit types.String, defaultVal, fieldName string, diags *diag.Diagnostics) string {
	if v := explicit.ValueString(); v != "" {
		return v
	}
	if defaultVal != "" {
		return defaultVal
	}
	diags.AddError(
		"Missing "+fieldName,
		fieldName+" was not specified and could not be resolved from the API key context.",
	)
	return ""
}
