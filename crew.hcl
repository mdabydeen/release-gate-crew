#!/usr/bin/env docker agent run
#
# The same release-gate crew as crew.yaml, written in HCL.
# docker agent converts this to the YAML shape internally and validates it
# against the same schema, so the two files must stay equivalent.
#
#   docker agent run crew.hcl "Is target/ ready to ship?"

metadata {
  author  = "mdabydeen"
  license = "MIT"
  readme  = "Release gate crew (HCL edition). See crew.yaml for the YAML twin."
}

model "lead" {
  provider   = "anthropic"
  model      = "claude-sonnet-4-5"
  max_tokens = 8000
}

model "worker" {
  provider   = "anthropic"
  model      = "claude-haiku-4-5"
  max_tokens = 4000
}

agent "root" {
  model       = "lead"
  description = "Release gate lead. Delegates reviews and makes the PASS/BLOCK call."
  instruction = file("prompts/lead.md")
  sub_agents  = ["container_auditor", "pipeline_auditor", "scribe"]

  toolset "think" {}
  toolset "todo" {}

  command "gate" {
    description = "Run the full release gate on target/"
    instruction = "Is target/ ready to ship? Run the full gate and write the report."
  }
}

agent "container_auditor" {
  model       = "worker"
  description = "Reviews Dockerfile and compose files for container risks."
  instruction = file("prompts/container_auditor.md")

  toolset "filesystem" {
    allow_list = ["./target"]
  }
  toolset "shell" {}
}

agent "pipeline_auditor" {
  model       = "worker"
  description = "Reviews CI workflow files for supply-chain and secret risks."
  instruction = file("prompts/pipeline_auditor.md")

  toolset "filesystem" {
    allow_list = ["./target"]
  }
}

agent "scribe" {
  model       = "worker"
  description = "Writes the release-gate report into reports/."
  instruction = file("prompts/scribe.md")

  toolset "filesystem" {
    allow_list = ["./reports"]
  }
}

# Unattended runs fail closed: safe calls run, anything unlisted is denied.
runtime {
  safety = "restricted"
}

permissions {
  allow = [
    "read_file",
    "read_multiple_files",
    "list_directory",
    "directory_tree",
    "search_files_content",
    "write_file:path=*reports/*",
    "shell:cmd=grep -n *",
  ]
  deny = [
    "shell:cmd=*curl*",
    "shell:cmd=*wget*",
    "shell:cmd=*rm *",
    "shell:cmd=*sudo*",
    "shell:cmd=*docker push*",
    "shell:cmd=*git push*",
    "shell:cmd=*;*",
    "shell:cmd=*&&*",
    "shell:cmd=*|*",
    "shell:cmd=*`*",
    "shell:cmd=*$(*",
  ]
}

# One shared pot for the whole team, so fanning out cannot multiply spend.
budget {
  max_cost = 0.50
  max_time = "10m"
}

flavors "offline" {
  providers = {
    stub = {
      api_type  = "openai_chatcompletions"
      base_url  = "http://127.0.0.1:8089/v1"
      token_key = "STUB_API_KEY"
    }
  }
  models = {
    lead   = { provider = "stub", model = "scripted" }
    worker = { provider = "stub", model = "scripted" }
  }
}

# Reproduces the prefix-glob footgun. See docs/FINDINGS.md.
flavors "naive-permissions" {
  permissions = {
    deny = ["shell:cmd=curl *", "shell:cmd=rm *"]
  }
}

flavors "cheap" {
  models = {
    lead = { model = "claude-haiku-4-5" }
  }
}
