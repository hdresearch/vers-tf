terraform {
  required_providers {
    vers = {
      source = "hdr/vers"
    }
  }
}

provider "vers" {
  # Reads from VERS_API_KEY env var automatically.
  # Or set explicitly:
  # api_key = var.vers_api_key
}
