variable "app_repo" {
  type    = string
  default = "https://github.com/yourorg/yourapp.git"
}

variable "app_branch" {
  type    = string
  default = "main"
}

variable "worker_count" {
  type    = number
  default = 3
}

variable "infra_url" {
  type        = string
  description = "Agent-services URL (optional)"
  default     = ""
}

variable "auth_token" {
  type      = string
  sensitive = true
  default   = ""
}
