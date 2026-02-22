package resources

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/hdr-is/terraform-provider-vers/internal/client"
)

var (
	_ resource.Resource              = &VMRestoreResource{}
	_ resource.ResourceWithConfigure = &VMRestoreResource{}
)

type VMRestoreResource struct {
	client *client.Client
}

type VMRestoreResourceModel struct {
	ID         types.String `tfsdk:"id"`
	CommitID   types.String `tfsdk:"commit_id"`
	State      types.String `tfsdk:"state"`
	SSHHost    types.String `tfsdk:"ssh_host"`
	SSHPrivateKey types.String `tfsdk:"ssh_private_key"`
	CreatedAt  types.String `tfsdk:"created_at"`
}

func NewVMRestoreResource() resource.Resource {
	return &VMRestoreResource{}
}

func (r *VMRestoreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm_restore"
}

func (r *VMRestoreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Restore a new VM from a previously created commit. The new VM starts with the exact state captured by the commit.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The restored VM ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"commit_id": schema.StringAttribute{
				Required:    true,
				Description: "The commit ID to restore from.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"state": schema.StringAttribute{
				Computed:    true,
				Description: "Current state of the restored VM.",
			},
			"ssh_host": schema.StringAttribute{
				Computed:    true,
				Description: "SSH hostname for the restored VM.",
			},
			"ssh_private_key": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "SSH private key for the restored VM.",
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Timestamp when the restored VM was created.",
			},
		},
	}
}

func (r *VMRestoreResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *VMRestoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VMRestoreResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	commitID := plan.CommitID.ValueString()

	tflog.Debug(ctx, "Restoring VM from commit", map[string]interface{}{"commit_id": commitID})

	result, err := r.client.RestoreVM(commitID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to restore VM from commit", err.Error())
		return
	}

	vmID := result.VMID
	plan.ID = types.StringValue(vmID)
	plan.SSHHost = types.StringValue(fmt.Sprintf("%s.vm.vers.sh", vmID))

	// Wait for the restored VM to be running
	if err := r.client.WaitForBoot(vmID, 3*time.Minute); err != nil {
		resp.Diagnostics.AddWarning("VM restored but may not be fully booted", err.Error())
	}

	// Fetch state
	vm, err := r.client.GetVM(vmID)
	if err != nil {
		resp.Diagnostics.AddWarning("Failed to read restored VM state", err.Error())
		plan.State = types.StringValue("unknown")
	} else if vm != nil {
		plan.State = types.StringValue(vm.State)
		plan.CreatedAt = types.StringValue(vm.CreatedAt)
	}

	// Fetch SSH key
	sshKey, err := r.client.GetSSHKey(vmID)
	if err != nil {
		resp.Diagnostics.AddWarning("Failed to fetch SSH key for restored VM", err.Error())
		plan.SSHPrivateKey = types.StringValue("")
	} else {
		plan.SSHPrivateKey = types.StringValue(sshKey.SSHPrivateKey)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *VMRestoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state VMRestoreResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vm, err := r.client.GetVM(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read restored VM", err.Error())
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

func (r *VMRestoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// commit_id change requires replacement. No in-place updates.
	var state VMRestoreResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *VMRestoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state VMRestoreResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Debug(ctx, "Deleting restored Vers VM", map[string]interface{}{"vm_id": state.ID.ValueString()})

	if err := r.client.DeleteVM(state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete restored VM", err.Error())
		return
	}
}
