package resources

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/hdresearch/vers-tf/internal/client"
)

var (
	_ resource.Resource              = &VMCommitResource{}
	_ resource.ResourceWithConfigure = &VMCommitResource{}
)

type VMCommitResource struct {
	client *client.Client
}

type VMCommitResourceModel struct {
	ID         types.String `tfsdk:"id"`
	VMID       types.String `tfsdk:"vm_id"`
	CommitID   types.String `tfsdk:"commit_id"`
	KeepPaused types.Bool   `tfsdk:"keep_paused"`
	Triggers   types.Map    `tfsdk:"triggers"`
}

func NewVMCommitResource() resource.Resource {
	return &VMCommitResource{}
}

func (r *VMCommitResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vm_commit"
}

func (r *VMCommitResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Snapshot a VM to a reusable commit. The commit_id can be used with vers_vm_restore to create new VMs from this state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Resource ID (same as commit_id).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vm_id": schema.StringAttribute{
				Required:    true,
				Description: "The VM ID to commit/snapshot.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"commit_id": schema.StringAttribute{
				Computed:    true,
				Description: "The resulting commit ID. Use this with vers_vm_restore.",
			},
			"keep_paused": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Keep the source VM paused after commit. Useful for golden images that don't need to keep running. Default: false.",
			},
			"triggers": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Map of arbitrary keys to values. When the values change, the commit is recreated. Use to trigger re-commit when provisioning changes.",
			},
		},
	}
}

func (r *VMCommitResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *VMCommitResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan VMCommitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vmID := plan.VMID.ValueString()
	keepPaused := plan.KeepPaused.ValueBool()

	tflog.Debug(ctx, "Committing Vers VM", map[string]interface{}{
		"vm_id": vmID, "keep_paused": keepPaused,
	})

	// Flush filesystem caches before snapshotting. The Vers commit API pauses
	// the VM and captures its memory + disk state. If the kernel still has
	// dirty pages in its buffer cache, they may appear as zero-filled regions
	// in the committed image — corrupting files written by prior provisioning.
	r.syncBeforeCommit(ctx, vmID)

	result, err := r.client.CommitVM(vmID, keepPaused)
	if err != nil {
		resp.Diagnostics.AddError("Failed to commit VM", err.Error())
		return
	}

	plan.CommitID = types.StringValue(result.CommitID)
	plan.ID = types.StringValue(result.CommitID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *VMCommitResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Commits are immutable snapshots — nothing to refresh.
	// Just preserve state as-is.
	var state VMCommitResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *VMCommitResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Triggers changed — need to re-commit. Destroy + Create handles this via RequiresReplace on vm_id.
	// For trigger-only changes, we re-commit in place.
	var plan VMCommitResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vmID := plan.VMID.ValueString()
	keepPaused := plan.KeepPaused.ValueBool()

	tflog.Debug(ctx, "Re-committing Vers VM (triggers changed)", map[string]interface{}{
		"vm_id": vmID,
	})

	// Flush filesystem caches before re-commit (same as Create — see comment there).
	r.syncBeforeCommit(ctx, vmID)

	result, err := r.client.CommitVM(vmID, keepPaused)
	if err != nil {
		resp.Diagnostics.AddError("Failed to re-commit VM", err.Error())
		return
	}

	plan.CommitID = types.StringValue(result.CommitID)
	plan.ID = types.StringValue(result.CommitID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *VMCommitResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Commits cannot be deleted via the API currently.
	// We just remove from state. The commit remains in Vers.
	tflog.Debug(ctx, "Removing commit from Terraform state (commits are retained in Vers)")
}

// syncBeforeCommit SSHes into the VM and runs 'sync' to flush all dirty
// pages to disk. This prevents a class of corruption where the Vers commit
// API snapshots the VM while the kernel still has unflushed buffer cache
// entries — which manifest as zero-filled file regions in the committed image.
//
// This is best-effort with a warning (not a hard error) because some VMs
// may not be SSH-reachable at commit time (e.g. the VM was paused externally).
// In practice, every Terraform workflow that does provision → commit will
// have the VM running and SSH-reachable.
func (r *VMCommitResource) syncBeforeCommit(ctx context.Context, vmID string) {
	sshKey, err := r.client.GetSSHKey(vmID)
	if err != nil {
		tflog.Warn(ctx, "Could not get SSH key for pre-commit sync (skipping)", map[string]interface{}{
			"vm_id": vmID, "error": err.Error(),
		})
		return
	}

	ssh, err := client.NewSSHClient(vmID, sshKey.SSHPrivateKey)
	if err != nil {
		tflog.Warn(ctx, "Could not create SSH client for pre-commit sync (skipping)", map[string]interface{}{
			"vm_id": vmID, "error": err.Error(),
		})
		return
	}
	defer ssh.Cleanup()

	tflog.Debug(ctx, "Running 'sync' on VM before commit to flush dirty pages", map[string]interface{}{"vm_id": vmID})
	if _, err := ssh.ExecWithTimeout("sync", 2*time.Minute); err != nil {
		tflog.Warn(ctx, "Pre-commit sync failed (VM may not be SSH-reachable)", map[string]interface{}{
			"vm_id": vmID, "error": err.Error(),
		})
		return
	}
	tflog.Debug(ctx, "Pre-commit sync complete", map[string]interface{}{"vm_id": vmID})
}
