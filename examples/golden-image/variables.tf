variable "vers_api_key" {
  type        = string
  sensitive   = true
  description = "Vers API key"
  default     = "" # Falls back to VERS_API_KEY env var
}

variable "infra_url" {
  type        = string
  description = "URL of the agent-services infra VM (e.g., https://<vm_id>.vm.vers.sh:3000)"
  default     = ""
}

variable "auth_token" {
  type        = string
  sensitive   = true
  description = "Auth token for agent-services"
  default     = ""
}

variable "worker_count" {
  type        = number
  description = "Number of worker VMs to spawn from the golden image"
  default     = 0
}
