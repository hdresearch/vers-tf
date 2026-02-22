package resources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/hdr-is/terraform-provider-vers/internal/client"
)

var (
	_ resource.Resource                = &ProvisionResource{}
	_ resource.ResourceWithConfigure   = &ProvisionResource{}
	_ resource.ResourceWithModifyPlan  = &ProvisionResource{}
)

type ProvisionResource struct {
	client *client.Client
}

type ProvisionResourceModel struct {
	ID       types.String `tfsdk:"id"`
	VMID     types.String `tfsdk:"vm_id"`
	Files    types.List   `tfsdk:"files"`
	Commands types.List   `tfsdk:"commands"`
	Triggers types.Map    `tfsdk:"triggers"`
}

// FileBlock represents a file to upload to the VM.
type FileBlock struct {
	Source      types.String `tfsdk:"source"`
	Content     types.String `tfsdk:"content"`
	Destination types.String `tfsdk:"destination"`
}

func NewProvisionResource() resource.Resource {
	return &ProvisionResource{}
}

func (r *ProvisionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provision"
}

func (r *ProvisionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Provision a Vers VM by uploading files and running commands via SSH-over-TLS. " +
			"This resource handles the Vers-specific SSH transport (openssl s_client ProxyCommand) automatically.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Resource ID (hash of provisioning state).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"vm_id": schema.StringAttribute{
				Required:    true,
				Description: "The VM ID to provision.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"files": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Files to upload to the VM. Specify either 'source' (local file path) or 'content' (inline string), plus 'destination' (remote path).",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"source": schema.StringAttribute{
							Optional:    true,
							Description: "Local file path to upload. Mutually exclusive with 'content'.",
						},
						"content": schema.StringAttribute{
							Optional:    true,
							Description: "Inline content to write to the destination. Mutually exclusive with 'source'. Supports templatefile() output.",
						},
						"destination": schema.StringAttribute{
							Required:    true,
							Description: "Remote path on the VM where the file will be written.",
						},
					},
				},
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"commands": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Shell commands to execute on the VM (in order). Run after files are uploaded.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.RequiresReplace(),
				},
			},
			"triggers": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Map of trigger values. When any value changes, the resource is replaced (re-provisioned). " +
					"Use filesha256() to track file content changes.",
			},
		},
	}
}

func (r *ProvisionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *ProvisionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ProvisionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vmID := plan.VMID.ValueString()
	tflog.Info(ctx, "Provisioning Vers VM", map[string]interface{}{"vm_id": vmID})

	// Get SSH credentials
	sshKey, err := r.client.GetSSHKey(vmID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get SSH key for VM", err.Error())
		return
	}

	ssh, err := client.NewSSHClient(vmID, sshKey.SSHPrivateKey)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create SSH client", err.Error())
		return
	}
	defer ssh.Cleanup()

	// Wait for VM to be reachable
	tflog.Debug(ctx, "Waiting for VM to be reachable via SSH")
	if err := ssh.WaitReachable(3 * time.Minute); err != nil {
		resp.Diagnostics.AddError("VM not reachable via SSH", err.Error())
		return
	}

	// Upload files
	if !plan.Files.IsNull() && !plan.Files.IsUnknown() {
		var files []FileBlock
		resp.Diagnostics.Append(plan.Files.ElementsAs(ctx, &files, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		for i, f := range files {
			dest := f.Destination.ValueString()

			if !f.Source.IsNull() && f.Source.ValueString() != "" {
				// Upload from local file
				src := f.Source.ValueString()
				tflog.Debug(ctx, fmt.Sprintf("Uploading file %d: %s -> %s", i+1, src, dest))
				if err := ssh.UploadFile(src, dest); err != nil {
					resp.Diagnostics.AddError(
						fmt.Sprintf("Failed to upload file %s -> %s", src, dest),
						err.Error(),
					)
					return
				}
			} else if !f.Content.IsNull() && f.Content.ValueString() != "" {
				// Write inline content
				tflog.Debug(ctx, fmt.Sprintf("Writing inline content to %s (%d bytes)", dest, len(f.Content.ValueString())))
				if err := ssh.WriteFile(dest, f.Content.ValueString()); err != nil {
					resp.Diagnostics.AddError(
						fmt.Sprintf("Failed to write content to %s", dest),
						err.Error(),
					)
					return
				}
			} else {
				resp.Diagnostics.AddError(
					fmt.Sprintf("File block %d: either 'source' or 'content' must be specified", i+1),
					"Each file block requires either a 'source' (local file path) or 'content' (inline string).",
				)
				return
			}
		}
	}

	// Run commands
	if !plan.Commands.IsNull() && !plan.Commands.IsUnknown() {
		var commands []string
		resp.Diagnostics.Append(plan.Commands.ElementsAs(ctx, &commands, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		for i, cmd := range commands {
			tflog.Info(ctx, fmt.Sprintf("Running command %d/%d: %s", i+1, len(commands), truncate(cmd, 100)))
			output, err := ssh.ExecWithTimeout(cmd, 10*time.Minute)
			if err != nil {
				resp.Diagnostics.AddError(
					fmt.Sprintf("Command %d failed: %s", i+1, truncate(cmd, 80)),
					fmt.Sprintf("Error: %s\nOutput: %s", err.Error(), truncate(output, 2000)),
				)
				return
			}
			tflog.Debug(ctx, fmt.Sprintf("Command %d output: %s", i+1, truncate(output, 500)))
		}
	}

	// Generate a stable ID from the provisioning inputs
	plan.ID = types.StringValue(r.computeID(ctx, plan))

	tflog.Info(ctx, "VM provisioning complete", map[string]interface{}{"vm_id": vmID})
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ProvisionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	// Provisioning is a one-shot action. Nothing to refresh from the VM.
	var state ProvisionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Verify the VM still exists
	vm, err := r.client.GetVM(state.VMID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to verify VM exists", err.Error())
		return
	}
	if vm == nil {
		// VM was deleted — remove provisioning resource
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ProvisionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Triggers changed — re-provision.
	var plan ProvisionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	vmID := plan.VMID.ValueString()
	tflog.Info(ctx, "Re-provisioning Vers VM (triggers changed)", map[string]interface{}{"vm_id": vmID})

	// Get SSH credentials
	sshKey, err := r.client.GetSSHKey(vmID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get SSH key for VM", err.Error())
		return
	}

	ssh, err := client.NewSSHClient(vmID, sshKey.SSHPrivateKey)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create SSH client", err.Error())
		return
	}
	defer ssh.Cleanup()

	// Wait for VM to be reachable
	if err := ssh.WaitReachable(3 * time.Minute); err != nil {
		resp.Diagnostics.AddError("VM not reachable via SSH", err.Error())
		return
	}

	// Upload files
	if !plan.Files.IsNull() && !plan.Files.IsUnknown() {
		var files []FileBlock
		resp.Diagnostics.Append(plan.Files.ElementsAs(ctx, &files, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		for _, f := range files {
			dest := f.Destination.ValueString()
			if !f.Source.IsNull() && f.Source.ValueString() != "" {
				if err := ssh.UploadFile(f.Source.ValueString(), dest); err != nil {
					resp.Diagnostics.AddError(fmt.Sprintf("Failed to upload file to %s", dest), err.Error())
					return
				}
			} else if !f.Content.IsNull() && f.Content.ValueString() != "" {
				if err := ssh.WriteFile(dest, f.Content.ValueString()); err != nil {
					resp.Diagnostics.AddError(fmt.Sprintf("Failed to write content to %s", dest), err.Error())
					return
				}
			}
		}
	}

	// Run commands
	if !plan.Commands.IsNull() && !plan.Commands.IsUnknown() {
		var commands []string
		resp.Diagnostics.Append(plan.Commands.ElementsAs(ctx, &commands, false)...)
		if resp.Diagnostics.HasError() {
			return
		}

		for idx, cmd := range commands {
			tflog.Info(ctx, fmt.Sprintf("Re-provisioning command %d/%d: %s", idx+1, len(commands), truncate(cmd, 100)))
			output, err := ssh.ExecWithTimeout(cmd, 10*time.Minute)
			if err != nil {
				resp.Diagnostics.AddError(
					fmt.Sprintf("Command %d failed: %s", idx+1, truncate(cmd, 80)),
					fmt.Sprintf("Error: %s\nOutput: %s", err.Error(), truncate(output, 2000)),
				)
				return
			}
		}
	}

	plan.ID = types.StringValue(r.computeID(ctx, plan))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ProvisionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Provisioning is not reversible — we just remove from state.
	// The VM itself is managed by vers_vm.
	tflog.Debug(ctx, "Removing provision resource from state")
}

// computeID generates a deterministic ID from the provisioning config.
func (r *ProvisionResource) computeID(ctx context.Context, plan ProvisionResourceModel) string {
	h := sha256.New()
	h.Write([]byte(plan.VMID.ValueString()))

	// Hash files
	if !plan.Files.IsNull() && !plan.Files.IsUnknown() {
		var files []FileBlock
		plan.Files.ElementsAs(ctx, &files, false)
		for _, f := range files {
			h.Write([]byte(f.Destination.ValueString()))
			if !f.Source.IsNull() {
				// Hash the file content for source files
				content, err := os.ReadFile(f.Source.ValueString())
				if err == nil {
					h.Write(content)
				} else {
					h.Write([]byte(f.Source.ValueString()))
				}
			}
			if !f.Content.IsNull() {
				h.Write([]byte(f.Content.ValueString()))
			}
		}
	}

	// Hash commands
	if !plan.Commands.IsNull() && !plan.Commands.IsUnknown() {
		var commands []string
		plan.Commands.ElementsAs(ctx, &commands, false)
		for _, cmd := range commands {
			h.Write([]byte(cmd))
		}
	}

	// Hash triggers
	if !plan.Triggers.IsNull() && !plan.Triggers.IsUnknown() {
		triggers := plan.Triggers.Elements()
		keys := make([]string, 0, len(triggers))
		for k := range triggers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			h.Write([]byte(k))
			if v, ok := triggers[k].(types.String); ok {
				h.Write([]byte(v.ValueString()))
			}
		}
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ModifyPlan implements custom plan behavior — force replacement when triggers change.
func (r *ProvisionResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// Skip if creating or destroying
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}

	var planModel, stateModel ProvisionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &planModel)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &stateModel)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Check if triggers changed
	if !triggersEqual(planModel.Triggers, stateModel.Triggers) {
		// Force replacement by setting a new unknown ID
		resp.Plan.SetAttribute(ctx, path.Root("id"), types.StringUnknown())
		resp.RequiresReplace = append(resp.RequiresReplace, path.Root("id"))
	}
}

func triggersEqual(a, b types.Map) bool {
	if a.IsNull() && b.IsNull() {
		return true
	}
	if a.IsNull() || b.IsNull() {
		return false
	}
	aElems := a.Elements()
	bElems := b.Elements()
	if len(aElems) != len(bElems) {
		return false
	}
	for k, av := range aElems {
		bv, ok := bElems[k]
		if !ok {
			return false
		}
		if av.(types.String).ValueString() != bv.(types.String).ValueString() {
			return false
		}
	}
	return true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
