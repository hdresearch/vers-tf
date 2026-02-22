// Package provider implements the Vers Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hdresearch/vers-tf/internal/client"
	"github.com/hdresearch/vers-tf/internal/datasources"
	"github.com/hdresearch/vers-tf/internal/resources"
)

var _ provider.Provider = &VersProvider{}

// VersProvider implements the Vers Terraform provider.
type VersProvider struct {
	version string
}

// VersProviderModel is the schema model for provider configuration.
type VersProviderModel struct {
	APIKey  types.String `tfsdk:"api_key"`
	BaseURL types.String `tfsdk:"base_url"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &VersProvider{version: version}
	}
}

func (p *VersProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "vers"
	resp.Version = p.version
}

func (p *VersProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Terraform provider for Vers (vers.sh) — declaratively manage Firecracker micro-VMs.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Vers API key. Can also be set via VERS_API_KEY environment variable.",
			},
			"base_url": schema.StringAttribute{
				Optional:    true,
				Description: "Vers API base URL. Defaults to https://api.vers.sh/api/v1. Can also be set via VERS_BASE_URL.",
			},
		},
	}
}

func (p *VersProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config VersProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Resolve API key: config > env
	apiKey := ""
	if !config.APIKey.IsNull() && !config.APIKey.IsUnknown() {
		apiKey = config.APIKey.ValueString()
	}
	if apiKey == "" {
		apiKey = os.Getenv("VERS_API_KEY")
	}
	if apiKey == "" {
		resp.Diagnostics.AddError(
			"Missing API Key",
			"A Vers API key must be provided via the 'api_key' provider attribute or the VERS_API_KEY environment variable.",
		)
		return
	}

	// Resolve base URL: config > env > default
	baseURL := ""
	if !config.BaseURL.IsNull() && !config.BaseURL.IsUnknown() {
		baseURL = config.BaseURL.ValueString()
	}
	if baseURL == "" {
		baseURL = os.Getenv("VERS_BASE_URL")
	}

	c := client.New(apiKey, baseURL)

	// Make client available to resources and data sources
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *VersProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		resources.NewVMResource,
		resources.NewVMCommitResource,
		resources.NewVMBranchResource,
		resources.NewVMRestoreResource,
		resources.NewProvisionResource,
	}
}

func (p *VersProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		datasources.NewVMsDataSource,
	}
}
