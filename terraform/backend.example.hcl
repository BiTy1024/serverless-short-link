bucket         = "your-tf-state-bucket"
key            = "short-link/terraform.tfstate"
region         = "eu-central-1"
dynamodb_table = "pr-redirect-service-tf-locks"
encrypt        = true
