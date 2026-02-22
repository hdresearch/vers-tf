package resources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/hdr-is/terraform-provider-vers/internal/client"
)

var (
	_ resource.Resource              = &VMBranchResource{}
	_ resource.ResourceWithConfigure = &VMBranchResource{}
)

type VMBranchResource struct {
	client *client.Client
}

type VMBranchResourceModel struct {
	ID         types.String `tfsdk:"id"`
	SourceVMID types.String `tfsdk:"source_vm_id"`
	State      types.String `tfsdk:"state"`
	SSHHost    types.String `tfsdk:"ssh_host"`
	SSHPrivateKey types.String `tfsdk:"ssh_private_key"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func NewVMBranchResource() resource.Resource {
	return &VMBranchResource{}
}

func (r *VMBranchResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm_branch"
}

func (r *VMBranchResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Clone a running VM via copy-on-write branching. The new VM starts with the same state as the source.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The new (branched) VM ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"source_vm_id": schema.StringAttribute{
				Required:    true,
				Description: "The VM ID to branch/clone from.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"state": schema.StringAttribute{
				Computed:    true,
				Description: "Current state of the branched VM.",
			},
			"ssh_host": schema.StringAttribute{
				Computed:    true,
				Description: "SSH hostname for the branched VM.",
			},
			"ssh_private_key": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "SSH private key for the branched VM.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Timestamp when the branched VM was created.",
			},
		},
	}
}

func (r *VMBranchResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *VMBranchResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VMBranchResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sourceID := plan.SourceVMID.ValueString()

	tflog.Debug(ctx, "Branching Vers VM", map[string]interface{}{"source_vm_id": sourceID})

	newVMID, err := r.client.BranchVM(sourceID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to branch VM", err.Error())
		return
	}

	plan.ID = types.StringValue(newVMID)
	plan.SSHHost = types.StringValue(fmt.Sprintf("%s.vm.vers.sh", newVMID))

	// Fetch state
	vm, err := r.client.GetVM(newVMID)
	if err != nil {
		resp.Diagnostics.AddWarning("Failed to read branched VM state", err.Error())
		plan.State = types.StringValue("unknown")
	} else if vm != nil {
		plan.State = types.StringValue(vm.State)
		plan.CreatedAt = types.StringValue(vm.CreatedAt)
	}

	// Fetch SSH key
	sshKey, err := r.client.GetSSHKey(newVMID)
	if err != nil {
		resp.Diagnostics.AddWarning("Failed to fetch SSH key for branched VM", err.Error())
		plan.SSHPrivateKey = types.StringValue("")
	} else {
		plan.SSHPrivateKey = types.StringValue(sshKey.SSHPrivateKey)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *VMBranchResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VMBranchResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vm, err := r.client.GetVM(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read branched VM", err.Error())
		return
	}

	if vm == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.State = types.StringValue(vm.State)
	state.CreatedAt = types.StringValue(vm.CreatedAt)
	state.SSHHost = types.StringValue(fmt.Sprintf("%s.vm.vers.sh", vm.VMID))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *VMBranchResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// source_vm_id changes require replacement. No in-place updates.
	var state VMBranchResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *VMBranchResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state VMBranchResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting branched Vers VM", map[string]interface{}{"vm_id": state.ID.ValueString()})

	if err := r.client.DeleteVM(state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete branched VM", err.Error())
		return
	}
}
