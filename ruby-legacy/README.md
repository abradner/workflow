# Workflow

A modern, Ruby-first ETL pipeline for migrating, transforming and adapting things!

Eventually this will be a pluggable toolkit, but right now there are 4 workflows:

### RenderTalos
**What:** Hydrates a talos cluster

**Why:** Look, the talos guides are great but sometimes it's better to see a working example

**How:** Generates a talos cluster of manifests from template configurations using `1Password` Secure Note IDs to prepare for Talos Linux cluster deployment and bootstrap procedures.

### GenerateArgocd
**What:** Generates the core App-of-Apps `Application` manifests for ArgoCD. 

**Why:** Beats doing it manually! 

**How:** Maps all workloads cleanly to their designated `project_name` environment spaces so Argo can self-heal them.

### SyncWorkloads
**What:** Synchronizes Kustomize manifests for all workloads in a Kustomize project. 

**Why:** Very niche, you probably won't ever use this. 

**How:** Discovers bases and overlays, morphs legacy `Ingress` specs into modern `HTTPRoute` GatewayAPI configurations, injects `ExternalSecret` manifests, binds registry pull-secrets via mutations, and writes pristine YAML to your destination directory.

### Sync1Password
**What:** Migrates from AWS Secrets Manager to 1Password

**Why:** My homelab has a 1password subscription and for my use it's better than AWS Secrets Manager. Some of the software I'm deploying has a copy of the secrets in AWS Secrets Manager, so I'm using tooling to do the work of migrating it

**How:** Extracts raw legacy credentials securely from AWS Secrets Manager. Unpacks JSON configs or opaque binary keystores, sanitizes identifiers, and provisions structured **Secure Notes** in your 1Password vault for GitOps ExternalSecrets to safely retrieve.

## Architecture

Operating loosely on an **Extract-Transform-Load (ETL)** pattern, every workflow orchestrated by `workflow.rb` is split into three bounded phases:
1. `hydrate`: Some workflows need some discovered context to run properly - this is a prerequisite for the act phase.
2. `act_phase`: Side-effect-free extraction and transformation. Predictable, pure logic that plans all operations and renders in memory. 
3. `commit_phase`: The execution loop where all strictly planned filesystem I/O, Vault mutations, and side-effects occur.
4. `discard_your_hand_and_draw_five_cards`: I miss dominion on isotropic. Good times.

## Usage

Drive the pipeline via the CLI entrypoint:

```bash
./workflow.rb [command] [--dry-run]
```

### Commands

*   `sync` 
    Synchronizes Kustomize manifests for all workloads. Discovers bases and overlays, morphs legacy `Ingress` specs into modern `HTTPRoute` GatewayAPI configurations, injects `ExternalSecret` manifests, binds registry pull-secrets via mutations, and writes pristine YAML to your destination directory.
*   `setup-argo` 
    Generates the core App-of-Apps `Application` manifests for ArgoCD. Maps all workloads cleanly to their designated `project_name` environment spaces so Argo can self-heal them.
*   `sync-1p` 
    Extracts raw legacy credentials securely from AWS Secrets Manager. Unpacks JSON configs or opaque binary keystores, sanitizes identifiers, and provisions structured **Secure Notes** in your 1Password vault for GitOps ExternalSecrets to safely retrieve.
*   `render-talos` 
    Hydrates template configurations with `1Password` Secure Note IDs to prepare for Talos Linux cluster deployment and bootstrap procedures.

## Configuration 

Setup your environmental constraints inside a `.env` file at the root. Everything is dynamically mapped off your project declarations:

```dotenv
PROJECT_NAME='wtf'               # what is the name of the thing you're working on?
SOURCE_ENV='dev3'                # Where we're extracting logic from
TARGET_ENVS='dev4,dev5'          # Where we're migrating and layering the workloads

# Resource URIs
REGISTRY_HOSTNAME='cr.infra.fqdn'
TLD='fancy.tld'
```

## Testing

```bash
rspec # you know the drill
```

Workflows get integration tested, services behaviourally tested and utils with the gnarly bits get unit tested.

Anything that 'leaks' from the test environment gets mocked at the IO boundary.

