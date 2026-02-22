// Package datasources implements Vers Terraform data sources.
package datasources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hdresearch/vers-tf/internal/client"
)

var _ datasource.DataSource = &VMsDataSource{}
var _ datasource.DataSourceWithConfigure = &VMsDataSource{}

type VMsDataSource struct {
	client *client.Client
}

type VMsDataSourceModel struct {
	VMs []VMDataModel `tfsdk:"vms"`
}

type VMDataModel struct {
	ID        types.String `tfsdk:"id"`
	State     types.String `tfsdk:"state"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func NewVMsDataSource() datasource.DataSource {
	return &VMsDataSource{}
}

func (d *VMsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vms"
}

func (d *VMsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "List all Vers VMs owned by the authenticated user.",
		Attributes: map[string]schema.Attribute{
			"vms": schema.ListNestedAttribute{
				Computed:    true,
				Description: "List of VMs.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "VM ID.",
						},
						"state": schema.StringAttribute{
							Computed:    true,
							Description: "VM state (booting, running, paused).",
						},
						"created_at": schema.StringAttribute{
							Computed:    true,
							Description: "Creation timestamp.",
						},
					},
				},
			},
		},
	}
}

func (d *VMsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", "Expected *client.Client")
		return
	}
	d.client = c
}

func (d *VMsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	vms, err := d.client.ListVMs()
	if err != nil {
		resp.Diagnostics.AddError("Failed to list VMs", err.Error())
		return
	}

	var state VMsDataSourceModel
	for _, vm := range vms {
		state.VMs = append(state.VMs, VMDataModel{
			ID:        types.StringValue(vm.VMID),
			State:     types.StringValue(vm.State),
			CreatedAt: types.StringValue(vm.CreatedAt),
		})
	}

	// Ensure empty list instead of null
	if state.VMs == nil {
		state.VMs = []VMDataModel{}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
