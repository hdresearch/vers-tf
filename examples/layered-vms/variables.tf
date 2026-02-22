variable "vers_api_key" {
  type        = string
  sensitive   = true
  description = "Vers API key"
  default     = "" # Falls back to VERS_API_KEY env var
}

variable "infra_url" {
  type        = string
  description = "URL of agent-services infra VM"
  default     = ""
}

variable "auth_token" {
  type        = string
  sensitive   = true
  description = "Auth token for agent-services"
  default     = ""
}

variable "app_repo" {
  type        = string
  description = "Git repo URL for the application"
  default     = "https://github.com/example/app.git"
}

variable "app_branch" {
  type        = string
  description = "Git branch to deploy"
  default     = "main"
}

variable "worker_count" {
  type        = number
  description = "Number of worker VMs to spawn from the app layer"
  default     = 2
}
