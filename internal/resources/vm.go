package resources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/hdresearch/vers-tf/internal/client"
)

var (
	_ resource.Resource              = &VMResource{}
	_ resource.ResourceWithConfigure = &VMResource{}
)

type VMResource struct {
	client *client.Client
}

type VMResourceModel struct {
	ID         types.String `tfsdk:"id"`
	VCPUCount  types.Int64  `tfsdk:"vcpu_count"`
	MemSizeMiB types.Int64  `tfsdk:"mem_size_mib"`
	FSSizeMiB  types.Int64  `tfsdk:"fs_size_mib"`
	WaitBoot   types.Bool   `tfsdk:"wait_boot"`
	State      types.String `tfsdk:"state"`
	SSHHost    types.String `tfsdk:"ssh_host"`
	SSHPrivateKey types.String `tfsdk:"ssh_private_key"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func NewVMResource() resource.Resource {
	return &VMResource{}
}

func (r *VMResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm"
}

func (r *VMResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Creates a root Firecracker VM on the Vers platform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The VM ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vcpu_count": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(1),
				Description: "Number of vCPUs. Default: 1.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"mem_size_mib": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(2048),
				Description: "RAM in MiB. Default: 2048.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"fs_size_mib": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Default:     int64default.StaticInt64(4096),
				Description: "Disk size in MiB. Default: 4096.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"wait_boot": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Wait for VM to finish booting before marking as created. Default: true.",
			},
			"state": schema.StringAttribute{
				Computed:    true,
				Description: "Current VM state (booting, running, paused).",
			},
			"ssh_host": schema.StringAttribute{
				Computed:    true,
				Description: "SSH hostname for the VM ({id}.vm.vers.sh).",
			},
			"ssh_private_key": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "SSH private key for the VM.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Timestamp when the VM was created.",
			},
		},
	}
}

func (r *VMResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *client.Client")
		return
	}
	r.client = c
}

func (r *VMResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VMResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vcpu := int(plan.VCPUCount.ValueInt64())
	mem := int(plan.MemSizeMiB.ValueInt64())
	fs := int(plan.FSSizeMiB.ValueInt64())

	config := client.VMConfig{
		VCPUCount:  &vcpu,
		MemSizeMiB: &mem,
		FSSizeMiB:  &fs,
	}

	tflog.Debug(ctx, "Creating Vers VM", map[string]interface{}{
		"vcpu_count": vcpu, "mem_size_mib": mem, "fs_size_mib": fs,
	})

	result, err := r.client.CreateVM(config, plan.WaitBoot.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create VM", err.Error())
		return
	}

	plan.ID = types.StringValue(result.VMID)
	plan.SSHHost = types.StringValue(fmt.Sprintf("%s.vm.vers.sh", result.VMID))

	// Fetch current state
	vm, err := r.client.GetVM(result.VMID)
	if err != nil {
		resp.Diagnostics.AddWarning("Failed to read VM state after creation", err.Error())
		plan.State = types.StringValue("unknown")
	} else if vm != nil {
		plan.State = types.StringValue(vm.State)
		plan.CreatedAt = types.StringValue(vm.CreatedAt)
	}

	// Fetch SSH key
	sshKey, err := r.client.GetSSHKey(result.VMID)
	if err != nil {
		resp.Diagnostics.AddWarning("Failed to fetch SSH key", err.Error())
		plan.SSHPrivateKey = types.StringValue("")
	} else {
		plan.SSHPrivateKey = types.StringValue(sshKey.SSHPrivateKey)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *VMResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VMResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vm, err := r.client.GetVM(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read VM", err.Error())
		return
	}

	// VM was deleted outside of Terraform
	if vm == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.State = types.StringValue(vm.State)
	state.CreatedAt = types.StringValue(vm.CreatedAt)
	state.SSHHost = types.StringValue(fmt.Sprintf("%s.vm.vers.sh", vm.VMID))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *VMResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// VM config is immutable — all config changes require replacement.
	// This method handles non-replacing updates (currently none that need API calls).
	var plan VMResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Preserve computed values from state
	var state VMResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	plan.ID = state.ID
	plan.SSHHost = state.SSHHost
	plan.SSHPrivateKey = state.SSHPrivateKey
	plan.State = state.State
	plan.CreatedAt = state.CreatedAt

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *VMResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state VMResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting Vers VM", map[string]interface{}{"vm_id": state.ID.ValueString()})

	if err := r.client.DeleteVM(state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete VM", err.Error())
		return
	}
}
