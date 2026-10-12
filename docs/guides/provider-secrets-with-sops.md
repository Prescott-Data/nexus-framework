---
icon: material/file-lock-outline
---

# Provider Secrets with SOPS

Registering an OAuth app with a provider is the slow part of onboarding it: a developer console, a redirect URL to paste, scopes to tick, a review queue, and at the end a client id and a client secret. This guide is about the thirty seconds after that: where the pair goes so that it is encrypted at rest from the moment you have it, how it reaches the Broker, how you rotate it a year later, and how to make the `nexus` CLI refuse any path that would leave it in plaintext.

It is written to be followed top to bottom the first time and used as a lookup afterwards. The [CLI reference](../reference/cli.md) has the exact flags; [Security-as-Code](security-as-code.md) has the surrounding GitOps workflow.

---

## The model in one paragraph

Provider files say **which** secrets a provider needs, as `${PLACEHOLDER}` references and nothing else. One [SOPS](https://github.com/getsops/sops) file holds **the values**, every one encrypted with a key you control (Google Cloud KMS, AWS KMS, age or PGP). `nexus plan` and `nexus apply` decrypt that file in memory, substitute the values into the manifest, and send the result to the Broker, which stores each secret in its own vault or [secret backend](provider-secret-backends.md). The plaintext then exists only transiently: in the provider's console when you copy it; in a temporary file that `sops edit` (and the first-time encrypt in Step 3) creates with owner-only permissions and removes when the editor closes; and in the CLI's memory for the length of a run. The CLI itself never writes plaintext to disk, and nothing in git, in a shell profile or in a CI log ever holds a secret. Disk controls on the editing machine still matter for those transient files: full-disk encryption, and a temp directory you control.

```
infra/
  providers/                 one file per provider, placeholders only
    github.yaml
    google-workspace.yaml
    x.yaml
  secrets.enc.yaml           every value ENC[...], keys visible, encrypted to your KMS key
.sops.yaml                   which key encrypts which file

            nexus plan -file infra/providers -secrets sops:infra/secrets.enc.yaml
                    │
     ┌──────────────┼───────────────────┐
     ▼              ▼                   ▼
 read providers   decrypt secrets.enc  GET /providers (live state)
 (placeholders)   in memory (KMS)      from the Broker
     └──────────────┴───────────────────┘
                    ▼
        expand, validate, diff, mask → the plan
                    ▼ (apply)
        POST / PATCH to the Broker → its vault / Secret Manager
```

---

## Why a SOPS file rather than the alternatives

| Where the values live | Encrypted at rest | Diffable in a PR | Needs cloud access to plan | Scales to 150 providers |
|---|---|---|---|---|
| Exported shell variables (`-secrets env`) | no: shell history, profile files, process listings | no | no | no |
| A plaintext `.env` or `secrets.yaml` in the repo | no | yes, and so is the secret | no | yes |
| One Secret Manager secret per value (`-secrets gcp-secret-manager:`) | yes | no: a change is invisible in git | yes, for every command | yes, with one `gcloud` call per value to create or rotate |
| **A SOPS file (`-secrets sops:`)** | **yes** | **yes: the key that changed shows, the value does not** | only to decrypt; `validate` needs nothing | **yes: one file, one editor session** |

SOPS encrypts the **values** of a YAML, JSON or dotenv file and leaves the **keys** readable. A reviewer sees that `NEXUS_PROVIDER_X_CLIENT_SECRET` changed in a commit and nothing more. The file is safe to copy, to commit, to back up and to send to a colleague, because nothing in it can be read without the key, and the key is a cloud KMS key whose access is an IAM decision rather than a file that can leak.

Secret Manager remains a good source when a team already keeps provider credentials there, and the Broker itself uses it as a backend. For the operator's working copy, the file wins on every row above.

---

## Prerequisites

- **sops 3.9 or later**, for the `sops encrypt` / `sops edit` subcommands used here (older releases spell them `sops -e` and plain `sops`). `brew install sops`, `apt install sops` on recent Debian and Ubuntu, or a binary from the [releases page](https://github.com/getsops/sops/releases). `sops --version` must print 3.9.0 or higher.
- **The `nexus` CLI** 0.6.6 or later: `go install github.com/Prescott-Data/nexus-framework/nexus-cli/cmd/nexus@latest`. The CLI links the SOPS library, so it decrypts without shelling out and without `sops` on the machine that runs `plan`; only editing needs the binary.
- **A key**, chosen in the next section, and the credentials to use it from the machine that runs `plan` and `apply`.
- **A Broker** reachable as `BROKER_BASE_URL` with its `API_KEY`, for `plan` and `apply`. `validate` needs neither.

---

## Step 1: choose and create the key

SOPS does not encrypt the file with the KMS key directly. It generates a random data key per file, encrypts every value with that, and encrypts the data key to each recipient you name. Decrypting is therefore one KMS call per run, not one per value, and adding a second recipient later does not re-encrypt the values.

### Google Cloud KMS (recommended for a GCP deployment)

One key ring, one key, and IAM on the key for the people and service accounts that may plan and apply. Through Terraform, which is how the rest of a Nexus deployment is managed:

```hcl
resource "google_kms_key_ring" "nexus" {
  name     = "nexus"
  location = "us-central1"            # same region as the Broker keeps KMS calls cheap and local
}

resource "google_kms_crypto_key" "providers" {
  name            = "providers"
  key_ring        = google_kms_key_ring.nexus.id
  rotation_period = "7776000s"        # 90 days; old versions stay usable for decryption
  lifecycle { prevent_destroy = true } # destroying the key destroys every file encrypted to it
}

# People who edit the file need to encrypt and decrypt.
resource "google_kms_crypto_key_iam_member" "sops_editors" {
  for_each      = toset(["user:operator@example.com"])
  crypto_key_id = google_kms_crypto_key.providers.id
  role          = "roles/cloudkms.cryptoKeyEncrypterDecrypter"
  member        = each.value
}

# CI only ever runs plan, which only decrypts: the narrower role.
resource "google_kms_crypto_key_iam_member" "ci_decrypt" {
  crypto_key_id = google_kms_crypto_key.providers.id
  role          = "roles/cloudkms.cryptoKeyDecrypter"
  member        = "serviceAccount:nexus-ci@my-project.iam.gserviceaccount.com"
}

output "sops_kms_key" { value = google_kms_crypto_key.providers.id }
```

The output is the resource id SOPS needs: `projects/my-project/locations/us-central1/keyRings/nexus/cryptoKeys/providers`. The `gcloud` equivalent, for a one-off:

```bash
gcloud kms keyrings create nexus --location us-central1 --project my-project
gcloud kms keys create providers --keyring nexus --location us-central1 --purpose encryption --project my-project
gcloud kms keys add-iam-policy-binding providers --keyring nexus --location us-central1 --project my-project \
  --member user:operator@example.com --role roles/cloudkms.cryptoKeyEncrypterDecrypter
```

SOPS and the CLI authenticate with **Application Default Credentials**: `gcloud auth application-default login` as an account in `sops_editors`. Two things trip people up on a machine with several Google accounts, and both have one-line fixes:

- ADC is one file, `~/.config/gcloud/application_default_credentials.json`, shared by every tool. `gcloud config configurations activate` changes what `gcloud` uses but **not** what SOPS, Terraform and the CLI use. If decryption says `PermissionDenied` for an account you did not expect, re-run `gcloud auth application-default login` as the right one.
- KMS calls are billed to the ADC's **quota project**, which may belong to another organisation if the credential was first created elsewhere. The symptom is `USER_PROJECT_DENIED` or a `cloudkms.googleapis.com` permission error naming a project you were not thinking about. Set `GOOGLE_CLOUD_QUOTA_PROJECT=my-project` in the environment of every command that touches the key, or `gcloud auth application-default set-quota-project my-project` once.

### AWS KMS

```bash
aws kms create-key --description "nexus provider secrets" --query KeyMetadata.Arn --output text
aws kms create-alias --alias-name alias/nexus-providers --target-key-id <key id>
```

Grant `kms:Encrypt`, `kms:Decrypt` and `kms:GenerateDataKey` on the key to the operators' roles. SOPS uses the default AWS credential chain (`AWS_PROFILE`, instance role, SSO session).

### age, for one person or a non-cloud deployment

[age](https://github.com/FiloSottile/age) is a file-encryption key pair with no service behind it. Fine for a solo operator or a lab; weaker for a team, because the private key is a file that has to be copied to every machine that plans.

```bash
age-keygen -o ~/.config/sops/age/keys.txt     # prints the public key: age1...
```

SOPS and the CLI read that path by default; set `SOPS_AGE_KEY_FILE` to use another. The public key is what goes into `.sops.yaml`.

### PGP

Supported, and the oldest of the four. Prefer age for the same job unless the team already runs PGP.

---

## Step 2: tell SOPS which key encrypts which file

A `.sops.yaml` at the repository root maps file paths to keys, so that `sops edit` and `sops encrypt` need no flags and every operator encrypts to the same recipients:

```yaml
# .sops.yaml  (repository root)
creation_rules:
  - path_regex: infra/.*\.enc\.ya?ml$
    gcp_kms: projects/my-project/locations/us-central1/keyRings/nexus/cryptoKeys/providers
    # or:  kms: arn:aws:kms:us-east-1:123456789012:key/...
    # or:  age: age1qyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqszqgpqyqs3290gq
```

`path_regex` is matched against the path **relative to the directory holding `.sops.yaml`**, which is why the file lives at the root and why you run `sops` from there. A file that matches no rule produces `no matching creation rules found` on encrypt; a file that matches the wrong rule is encrypted to the wrong key and only fails much later. Keep one rule per environment if you keep one file per environment (see [Several environments](#several-environments)).

Two or more keys on a rule (`gcp_kms` and `age`, say) make every recipient able to decrypt: a KMS key for the team and an age key as a break-glass copy held offline is a common pairing.

---

## Step 3: write the file the first time

### Naming the entries

A placeholder can be any name, but one convention keeps every provider file self-explanatory and lets the `gcp-secret-manager` source derive ids from the same names should you ever switch:

```
NEXUS_PROVIDER_<NAME>_CLIENT_ID
NEXUS_PROVIDER_<NAME>_CLIENT_SECRET
```

where `<NAME>` is the provider's `name` upper-cased with `-` replaced by `_`: provider `google-workspace` uses `NEXUS_PROVIDER_GOOGLE_WORKSPACE_CLIENT_ID`. A SAML provider has no secret, so it has no entries. A static provider (`api_key`, `basic_auth`, `header`, `query_param`, `hmac_payload`, `aws_sigv4`) usually has none either, because its credential is captured per connection, not registered with the provider; if its `params` carry a shared secret such as a webhook signing key, give that a placeholder too.

### Formats

YAML is the natural choice (keys stay readable, comments are allowed). JSON and dotenv (`KEY=value`, extension `.env`) work the same way. In every format the file must be a **flat** mapping of placeholder name to scalar value: a nested mapping is reported as `key "X": value must be a scalar` rather than flattened silently, because a nested structure is always a mistake in this file.

A value that looks like a number (a 19-digit client id, say) is kept as its exact text; nothing is rounded through a float. A value that must keep leading or trailing whitespace should be quoted in YAML; otherwise plain scalars are fine.

### Creating it

Never write the plaintext inside the repository, even for a moment: an editor backup, a crash or a stray `git add -A` is how a plaintext file gets committed. Write it in a private temporary directory (owner-only, on an encrypted volume), encrypt from there, and remove the directory in the same breath:

```bash
TMP="$(mktemp -d)" && chmod 700 "$TMP"
cat > "$TMP/secrets.yaml" <<'EOT'
NEXUS_PROVIDER_GITHUB_CLIENT_ID: <client id from the GitHub app settings page>
NEXUS_PROVIDER_GITHUB_CLIENT_SECRET: <client secret generated on that page>
NEXUS_PROVIDER_X_CLIENT_ID: <client id from the X developer portal>
NEXUS_PROVIDER_X_CLIENT_SECRET: <client secret from the X developer portal>
EOT

# --filename-override makes .sops.yaml match the destination path, not the temp path
sops encrypt --filename-override infra/secrets.enc.yaml --output infra/secrets.enc.yaml "$TMP/secrets.yaml"
rm -rf "$TMP"
```

Or let SOPS create it: `sops edit infra/secrets.enc.yaml` on a path that does not exist opens an editor with example content; delete the example, type your entries, save, and the encrypted file is written. Either way the result looks like this:

```yaml
NEXUS_PROVIDER_GITHUB_CLIENT_ID: ENC[AES256_GCM,data:4F2d...,iv:...,tag:...,type:str]
NEXUS_PROVIDER_GITHUB_CLIENT_SECRET: ENC[AES256_GCM,data:9aK1...,iv:...,tag:...,type:str]
NEXUS_PROVIDER_X_CLIENT_ID: ENC[AES256_GCM,data:...,type:str]
NEXUS_PROVIDER_X_CLIENT_SECRET: ENC[AES256_GCM,data:...,type:str]
sops:
    gcp_kms:
        - resource_id: projects/my-project/locations/us-central1/keyRings/nexus/cryptoKeys/providers
          created_at: "2026-10-12T09:14:02Z"
          enc: CiQA...
    lastmodified: "2026-10-12T09:14:02Z"
    mac: ENC[AES256_GCM,data:...,type:str]
    version: 3.13.3
```

Every value is ciphertext; every key is readable; the `sops` block records the recipients, the encrypted data key and a MAC over the whole file, so an edit made outside `sops` is detected rather than silently decrypted. The CLI ignores the `sops` block when it reads the file.

### Where to put it

Beside the provider directory, **not inside it**. In directory mode the CLI treats every `.yaml` in `-file` as a provider, and a secrets file there is reported as a provider with no `name`. `infra/secrets.enc.yaml` next to `infra/providers/` is the layout used throughout this guide.

---

## Step 4: reference the values from provider files

```yaml
# infra/providers/github.yaml
name: github
auth_type: oauth2
client_id: ${NEXUS_PROVIDER_GITHUB_CLIENT_ID}
client_secret: ${NEXUS_PROVIDER_GITHUB_CLIENT_SECRET}
auth_url: https://github.com/login/oauth/authorize
token_url: https://github.com/login/oauth/access_token
scopes: [read:user, user:email]
```

A placeholder is `${NAME}` (or `$NAME`) anywhere inside a string value, including under `params`; keys and comments are never expanded. A bare placeholder takes the type its value implies (`${FLAG}` with `FLAG=true` becomes a boolean); a quoted one stays a string. A value that is partly literal, such as `https://${TENANT}.example.com/oauth`, is expanded like any other.

Check the shape before touching any secret:

```bash
nexus validate -file infra/providers
```

```
Read 3 providers from infra/providers (3 files)
6 placeholders used (not resolved; pass -secrets to check them)
✓ Manifest is valid
```

---

## Step 5: run the CLI against the file

```bash
export BROKER_BASE_URL=https://nexus-broker.example.com
export API_KEY="$(…the Broker's API key, from wherever you keep it…)"
export NEXUS_SECRETS=sops:infra/secrets.enc.yaml     # or pass -secrets each time

nexus validate -file infra/providers -secrets "$NEXUS_SECRETS"   # resolves every placeholder, sends nothing
nexus plan     -file infra/providers                               # diff against the Broker
nexus apply    -file infra/providers                               # after reading the plan
```

What happens on `plan`: the provider files are read with placeholders kept, to learn which names are needed; the SOPS file is decrypted in memory with one KMS call; only those names are taken from it; the files are read again with the values substituted; the Broker's rules are checked; the live providers are fetched and diffed; the plan is printed with every secret masked:

```
Read 3 providers from infra/providers (3 files)

--- Execution Plan ---
+ CREATE : x
~ UPDATE : github
    client_secret: *** → ***
= OK     : google-workspace (no changes)

Plan complete. Run 'nexus apply' to perform these actions.
```

`client_secret` is never returned by the Broker. Since 0.6.4 it returns a fingerprint of what it stores, `plan` fingerprints the manifest value, and the two are compared, so a rotated value in the file is ordinary drift and an unchanged one is not sent again. A provider registered before 0.6.4 has no fingerprint until the Broker next uses or rotates its secret and is not compared until then.

`validate` deliberately ignores `NEXUS_SECRETS`: a validate in CI must never decrypt or call anything unless the `-secrets` flag is given explicitly.

---

## Step 6: day to day

### Add a provider

1. Register the app with the provider; copy the pair.
2. `sops edit infra/secrets.enc.yaml`, add the two lines, save.
3. Add `infra/providers/<name>.yaml` with the two placeholders.
4. `nexus validate -file infra/providers -secrets sops:infra/secrets.enc.yaml`, then `plan`, then `apply`.

### Rotate a secret

1. Generate the new secret in the provider's console. Most providers keep the old one valid until you delete it; do not delete it yet.
2. `sops edit infra/secrets.enc.yaml`, replace the value, save. The diff shows the one changed line (plus `lastmodified` and `mac`), never the value.
3. `nexus plan` shows `~ UPDATE : <name>` with `client_secret: *** → ***`. `nexus apply` sends only that.
4. Delete the old secret in the provider's console.

### Remove a provider

Delete the provider file; `nexus plan -prune` lists the provider under `!`/`-`; `nexus apply -prune` deletes it, and every connection tied to it stops working. Then remove its two lines from the secrets file; a stale entry is harmless but misleading.

### Editing: the editor

`sops edit` decrypts to a temporary file, opens `$EDITOR` (or `$SOPS_EDITOR`), waits for the editor to exit, re-encrypts and deletes the temporary file. For a GUI editor the command must block until the tab is closed:

```bash
EDITOR="code --wait" sops edit infra/secrets.enc.yaml      # VS Code
EDITOR="subl --wait" sops edit infra/secrets.enc.yaml      # Sublime Text
```

Without `--wait` the editor returns immediately, `sops` sees an unchanged file and writes nothing. Opening `infra/secrets.enc.yaml` directly in an editor shows the `ENC[...]` strings: that is the ciphertext, not a bug, and editing it by hand corrupts the MAC.

While the editor is open, the decrypted content is a plaintext file in the system temp directory (`$TMPDIR`, or `/tmp`), created by `sops` with owner-only permissions and deleted when the editor exits. That is the one moment a secret is on disk, so: keep that directory on an encrypted volume (the default on current macOS and on Linux with full-disk encryption), close the editor as soon as you have saved, and be aware that an editor's own backup or swap files can outlive the session; disable them for that path (`code --wait` keeps none; for vim, `set noswapfile nobackup nowritebackup` on the sops temp path).

A one-line change without an editor: `sops set infra/secrets.enc.yaml '["NEXUS_PROVIDER_X_CLIENT_SECRET"]' '"new-value"'`. Note that the value then passes through your shell history; prefer `sops edit` for secrets.

---

## Enforcing encrypted secrets

Everything above works with the CLI's permissive defaults, which exist so that a first run on a laptop needs nothing but exported variables. Those defaults also leave three doors open: `-secrets env` is accepted, a placeholder the file lacks is quietly taken from the environment, and a provider file may carry `client_secret: s3cret` literally. Each is a way for a secret to sit in plaintext at rest (a `.zshrc`, a CI variable, a committed file) without anyone noticing, and the apply still succeeds.

`-require-encrypted-secrets` closes all three. Set it once for every command:

```bash
export NEXUS_REQUIRE_ENCRYPTED_SECRETS=true
```

| What is refused | Where | The message |
|---|---|---|
| A source that is not encrypted at rest (`env`, or no `-secrets` at all on `plan`/`apply`) | `validate -secrets`, `plan`, `apply` | `-require-encrypted-secrets: -secrets is "env", which is not encrypted at rest; use sops:<path> or gcp-secret-manager:<project>` |
| Environment fallback: a placeholder missing from the file is unresolved even when the shell exports it | `validate -secrets`, `plan`, `apply` | `${NEXUS_PROVIDER_X_CLIENT_SECRET}: not found (looked for key NEXUS_PROVIDER_X_CLIENT_SECRET in infra/secrets.enc.yaml)`, with no "then environment variable" clause |
| A `client_secret` that is not exactly one `${PLACEHOLDER}`: a literal, or a literal with a placeholder mixed in (`hunter2-${X}` is the whole secret once `X` resolves to nothing) | `validate` even without `-secrets`, `plan`, `apply` | `infra/providers/x.yaml: x: client_secret: not exactly one ${PLACEHOLDER}; -require-encrypted-secrets refuses a literal or partly literal value in the manifest` |

`plan` and `apply` run these before any secret is decrypted and before the Broker is contacted, and the literal-secret failure ends with how many providers are affected and which source to move the values into. The literal value itself is never printed. A SOPS file that was never encrypted is caught earlier still: decryption reports that it has no SOPS metadata, so renaming a plaintext file to `.enc.yaml` does not get it past the policy. And because SOPS can be told to leave chosen keys in plaintext (`unencrypted_suffix`, `unencrypted_regex`, `encrypted_regex` in `.sops.yaml` or on the command line), the file is also inspected as stored: any value the manifest asks for that is not an `ENC[...]` ciphertext stops the run, by key name only.

"Exactly one" is judged on the value as written, with the grammar the manifest is expanded with: any non-empty braced name counts, and text or whitespace around the placeholder (including whitespace kept by quoting) makes it partly literal. This is a policy on those three paths, not a scan for secrets. Two things are deliberately not policed. `client_id` is not a secret, although `plan` masks it. `params` is masked as a whole in every plan and in the Broker's audit log, but the CLI cannot know which of its keys are credentials for a given provider, so a literal under `params` is accepted; keep credentials there behind placeholders by convention.

Where to turn it on:

- **In CI**, on every pull request: `nexus validate -file infra/providers -require-encrypted-secrets`. No key, no Broker, but a commit that pastes a secret into a provider file fails before a reviewer with a key has to look at it.
- **In the seed script** and the operator's shell: `export NEXUS_REQUIRE_ENCRYPTED_SECRETS=true` above the `plan` and `apply` calls, so nobody's exported variable can silently win over the file.

---

## CI

`validate` needs no secrets and runs on every pull request. `plan` needs the Broker and the key; run it where the pipeline can reach both, as an informational check that shows reviewers the diff. Apply from a trusted environment, by hand, after reading the plan. On GitHub Actions with Google Cloud and Workload Identity Federation, so that no key file is stored as an Actions secret:

```yaml
on: [pull_request]
permissions:
  contents: read
  id-token: write          # for Workload Identity Federation

jobs:
  providers:
    runs-on: ubuntu-latest
    env:
      NEXUS_REQUIRE_ENCRYPTED_SECRETS: "true"
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - run: go install github.com/Prescott-Data/nexus-framework/nexus-cli/cmd/nexus@v0.6.6

      # Needs no key, no Broker: shape, duplicates, and no literal secrets.
      - run: nexus validate -file infra/providers

      # The rest only where the pipeline is allowed to decrypt.
      - uses: google-github-actions/auth@v2
        with:
          workload_identity_provider: projects/123456789/locations/global/workloadIdentityPools/github/providers/github
          service_account: nexus-ci@my-project.iam.gserviceaccount.com
      - run: nexus validate -file infra/providers -secrets sops:infra/secrets.enc.yaml
      - run: nexus plan -file infra/providers -secrets sops:infra/secrets.enc.yaml
        env:
          BROKER_BASE_URL: ${{ secrets.BROKER_BASE_URL }}
          API_KEY: ${{ secrets.BROKER_API_KEY }}
          GOOGLE_CLOUD_QUOTA_PROJECT: my-project
```

The service account needs only `roles/cloudkms.cryptoKeyDecrypter` on the key: `plan` decrypts and never encrypts, so CI should not be able to re-encrypt the file. People who edit it get `roles/cloudkms.cryptoKeyEncrypterDecrypter`. The only Actions secrets are the Broker's URL and API key; the provider secrets never leave the file.

---

## Several environments

One file per environment, one rule per file, and ideally one key per environment so that the people who may apply to production are exactly the people who can decrypt production's file:

```yaml
# .sops.yaml
creation_rules:
  - path_regex: infra/secrets\.prod\.enc\.ya?ml$
    gcp_kms: projects/my-project/locations/us-central1/keyRings/nexus/cryptoKeys/providers-prod
  - path_regex: infra/secrets\.staging\.enc\.ya?ml$
    gcp_kms: projects/my-project/locations/us-central1/keyRings/nexus/cryptoKeys/providers-staging
```

The provider files are the same for both environments when the placeholder names are the same; only the file passed to `-secrets` differs, together with `BROKER_BASE_URL`:

```bash
nexus plan -file infra/providers -secrets sops:infra/secrets.staging.enc.yaml
nexus plan -file infra/providers -secrets sops:infra/secrets.prod.enc.yaml
```

If an environment uses a different OAuth app for some providers but the same for others, the same file layout still works: every environment's file holds every placeholder, and the ones that are shared simply hold the same value.

---

## Key management

- **Adding or removing a recipient.** Edit `.sops.yaml`, then `sops updatekeys infra/secrets.enc.yaml`. SOPS re-encrypts the data key to the new recipient set; the values are untouched and the diff is the `sops` block only. Anyone removed can still read the old commit, so rotate the secrets themselves if that matters.
- **KMS key version rotation.** Cloud KMS rotates key versions on the schedule you set and keeps old versions for decryption; a file encrypted to version 3 still decrypts after version 4 becomes primary. Nothing to do on the SOPS side. `sops rotate -i infra/secrets.enc.yaml` generates a fresh data key and re-encrypts every value with it, which you want after a recipient is removed or on a schedule of your own.
- **Destroying a key version** that a file was encrypted under destroys the file. The Terraform `prevent_destroy` above exists for this reason; so does the age key held offline as a second recipient.
- **Losing access** (a new laptop, a revoked account) means `PermissionDenied` or `no key could decrypt`. The fix is IAM on the key, not anything in the repository.

---

## To commit the ciphertext, or not

SOPS is designed so that the encrypted file **can** be committed: that is what makes a rotation a reviewable change with history, lets a new operator clone and run, and keeps the file backed up with the rest of the repository. The ciphertext is safe to publish in a private repository; it reveals which providers exist and when their secrets last changed, which the provider files reveal anyway.

Some teams prefer to keep even the ciphertext out of git, for policy reasons or because the repository is more widely shared than the key. That works, with three consequences to be aware of: the Broker becomes the only record of what is live, so `nexus plan` is how you discover that your copy and a colleague's differ; a new operator needs the file from someone who has it (sharing it is safe) or has to rebuild it from the Broker's own backend; and the file must be in `.gitignore` with a pattern that also catches any plaintext you might write beside it:

```gitignore
infra/secrets.enc.yaml
infra/secrets.yaml
infra/*.dec.yaml
infra/*.plain.yaml
infra/providers/secrets*.yaml
```

Whichever you choose, the plaintext patterns belong in `.gitignore` regardless, and `-require-encrypted-secrets` belongs in CI regardless.

---

## What the CLI guarantees about the plaintext

- The CLI decrypts in memory, through the SOPS library: no temporary file, no subprocess, nothing written to disk. (`sops edit` is the exception, by design of editing: a transient plaintext temp file that `sops` deletes afterwards, see Step 6.)
- Only the placeholders the provider files use are taken from the decrypted content; the rest is dropped.
- `plan` masks `client_secret`, `client_id`, the whole of `params`, and every other field that held a placeholder in the manifest, on both sides of a diff, as `*** → ***`.
- Every validation message is passed through a redaction step that replaces any resolved value with its placeholder, including values a YAML decoder would otherwise quote in a type error and quoted scalars it would truncate.
- A malformed line in a decrypted dotenv file is reported by line number only.
- A URL that fails to parse is reported by reason only, never by the input.
- The Broker's audit log masks every value under `params`, keeping the keys.
- Exit code `1` on any unresolved placeholder or policy failure, before the Broker is contacted, so a pipeline cannot half-apply.

What the CLI cannot do is protect a machine that is already compromised while `plan` runs, or stop someone who holds the KMS decrypt role from reading the file. Those are the two risks the key's IAM policy is for.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `error loading config: no matching creation rules found` on `sops encrypt` | The file's path does not match any `path_regex` in `.sops.yaml`, usually because you are encrypting a temp file or running outside the repository root | Run from the root; pass `--filename-override infra/secrets.enc.yaml` when the input is a temp file or stdin |
| `PermissionDenied` / `permission 'cloudkms.cryptoKeyVersions.useToDecrypt' denied` | ADC is a different account than you think, or the account lacks the role on the key | `gcloud auth application-default login` as a member of `sops_editors`; check the key's IAM, not the project's |
| `USER_PROJECT_DENIED` or an error naming a project you did not choose | KMS calls are billed to ADC's quota project, which is another project | `export GOOGLE_CLOUD_QUOTA_PROJECT=my-project`, or `gcloud auth application-default set-quota-project my-project` |
| `Reauthentication is needed` / `invalid_grant` | The ADC token expired (Workspace accounts expire daily on some policies) | `gcloud auth application-default login` again |
| `decrypt infra/secrets.enc.yaml: sops metadata not found` | The file was never encrypted; it is plaintext with an `.enc` name | Encrypt it (Step 3); the CLI refuses to read it as is, by design |
| `MAC mismatch` | The ciphertext was edited outside `sops`, or a merge conflict was resolved by hand | Take the file from the last good commit and redo the change with `sops edit` |
| `no key could decrypt the data key` | None of the recipients in the `sops` block is reachable from this machine | Wrong account or key; or the file was encrypted to a different environment's key |
| `${NAME}: not found (looked for key NAME in infra/secrets.enc.yaml)` | The provider file and the secrets file spell the name differently | Match them exactly; the convention is `NEXUS_PROVIDER_<NAME>_CLIENT_ID`/`_CLIENT_SECRET` with `-` as `_` |
| `key "X": value must be a scalar` | A nested mapping in the secrets file | Flatten it: one `NAME: value` per line |
| `dotenv line 7: expected KEY=value` | A line without `=` in a `.env` secrets file | Fix line 7; the line itself is deliberately not printed |
| `infra/providers/secrets.enc.yaml: providers[0]: name: missing required field` | The secrets file was placed inside the provider directory, so the CLI read it as a provider | Move it beside the directory, e.g. `infra/secrets.enc.yaml` |
| The editor opens and shows `ENC[...]` strings | You opened the file directly, not through `sops edit` | `sops edit infra/secrets.enc.yaml` |
| `sops edit` returns at once and nothing changed | A GUI editor that does not block | `EDITOR="code --wait"` (or the editor's equivalent) |
| `-require-encrypted-secrets: -secrets is "env"` on `plan` | `NEXUS_SECRETS` is unset and no `-secrets` was passed, so the default `env` source was selected | Pass `-secrets sops:…` or export `NEXUS_SECRETS` |
| `-require-encrypted-secrets: N value(s) are stored in plaintext in …: KEY` | A SOPS `unencrypted_suffix` / `unencrypted_regex` / `encrypted_regex` rule left that key in plaintext in the file | Remove the rule for that key (or rename the key) and `sops updatekeys` / re-encrypt; the value must be `ENC[...]` in the stored file |
| `plan` shows `client_secret: *** → ***` for a provider you did not rotate | The Broker's stored secret differs from the file (someone rotated through the API, or the file is stale) | Decide which is right; `apply` sends the file's value, so update the file first if the Broker is right |
| `plan` is silent after a rotation for an old provider | The provider has no fingerprint yet (registered before 0.6.4) | Rotate once with `PATCH /providers/{id}`, or wait for the Broker to use the secret; from then on `plan` compares it |
