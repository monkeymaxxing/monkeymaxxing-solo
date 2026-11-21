# Create your server and Cloudflare

**When:** right after "Your app". This is part 2, "Your server and
Cloudflare".

**What this file does:** the person creates one Debian 13 server, `solo01`,
at any company. You connect the laptop to it over WireGuard, so its public
SSH port can close in the next part. The systems repository gets a private
copy at GitHub and its vault. One command sets up everything at
Cloudflare. Every secret goes into the vault at a hidden prompt.

**Who acts:** the person creates the server, makes a GitHub repository when
`gh` is not signed in, chooses the vault passphrase, makes the Cloudflare
key, runs two commands in their own terminal, and types one GitHub key. You
do everything else.

**Tell the person when this starts:** the server screen in step 2 follows
the compact map in the same reply.

## Before you start

- **Commands the person runs** follow
  [`AGENTS.md`, "Commands the person runs"](../AGENTS.md#commands-the-person-runs):
  one code block, ready to paste, that starts with
  `# In your own terminal window, not through me:`.
- **Your own `mmx` commands** follow
  [`AGENTS.md`, "Running MMX from the laptop"](../AGENTS.md#running-mmx-from-the-laptop).
- **Names in the examples:** `acme_systems` is the systems repository,
  `shop` the app, `example.org` the domain, and `203.0.113.10` the server's
  public address.

## Steps

1. **The laptop's two keys.** Make the SSH key that logs in to `solo01`,
   and the laptop's WireGuard key. Keep an existing key: never replace one.

   ```sh
   # On the laptop.
   test -f ~/.ssh/solo01 || ssh-keygen -t ed25519 -f ~/.ssh/solo01 -N "" -C "$USER on the laptop"
   cat ~/.ssh/solo01.pub
   ~/.local/bin/doubleshell create-wireguard-key
   ```

   The SSH key has no passphrase, because MMX logs in without a prompt.
   The laptop's disk encryption, such as FileVault, protects it.
   `create-wireguard-key` saves the private key in
   `~/.config/doubleshell/wireguard_private_key` and prints the public key.

   Write the SSH public key line into `root_ssh_public_keys` in
   `mmx_project_settings.yml`, and the WireGuard public key into
   `laptop_wireguard_public_key` in `inventories/servers.yml`.

   **Check:** both values are written, and `~/.ssh/solo01` has mode `600`.

2. **The server.** The person creates the server at the company they chose,
   with the SSH public key from step 1. Find the company's own words for
   the screen from its public pages, such as Hetzner's "Add Server" and
   "SSH keys".

   **Ask the person,** with the company's name and the key line filled in:

   ```text
   Your server, at Hetzner

   What this is for:  your app, database, and backups run on this one server.

   Do this now:
     1. Sign in at https://console.hetzner.com (or sign up).
     2. Create a server: Debian 13, x86 (Intel or AMD), 2 CPUs, 4 GB memory, about 40 GB disk,
        in the location nearest your users, with a public IPv4 address.
     3. Under SSH keys, add this key, named "solo01 on my laptop":

        ssh-ed25519 AAAA... sam on the laptop

     4. Name the server solo01, and create it.

   Then paste the server's public IPv4 address here, such as 203.0.113.10.
   ```

   An address is not a secret, so the person pastes it in the chat.

   **Check:** you recorded the address. Write it into
   `public_ipv4_address` in `inventories/servers.yml`.

3. **The private connection to the server.** Over the company's temporary
   public SSH, check the server, install WireGuard, give `solo01` its own
   WireGuard key, and start WireGuard with the laptop as its only peer. The
   redirect from UDP 443 to WireGuard's port 51820 lets hotel and office
   networks through. Everything here is replaced by MMX's own settings in
   the next part.

   ```sh
   # On the laptop, in the systems repository. ADDRESS: the server's public IPv4 address.
   # LAPTOP_KEY: laptop_wireguard_public_key from inventories/servers.yml.
   ssh -i ~/.ssh/solo01 -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new \
       -o UserKnownHostsFile=~/.ssh/solo01_first_connection_known_hosts \
       root@ADDRESS bash -s -- LAPTOP_KEY <<'SET_UP_THE_PRIVATE_CONNECTION'
   set -eu
   laptop_wireguard_public_key=$1
   grep -q '^VERSION_ID="13"$' /etc/os-release || { echo "This server is not Debian 13." >&2; exit 1; }
   [ "$(uname -m)" = x86_64 ] || { echo "This server is not an x86 (amd64) server." >&2; exit 1; }
   export DEBIAN_FRONTEND=noninteractive
   apt-get update -q
   apt-get install -y -q wireguard-tools nftables
   umask 077
   [ -s /etc/wireguard/wg0.private_key ] || wg genkey > /etc/wireguard/wg0.private_key
   cat > /etc/wireguard/wg0.conf <<WG0
   [Interface]
   PrivateKey = $(cat /etc/wireguard/wg0.private_key)
   Address = 10.20.1.1/16
   ListenPort = 51820

   [Peer]
   PublicKey = $laptop_wireguard_public_key
   AllowedIPs = 10.20.10.1/32
   WG0
   cat > /etc/wireguard/wireguard_udp_443_redirect.nft <<'NFT'
   table inet solo_wireguard_port {}
   flush table inet solo_wireguard_port
   table inet solo_wireguard_port {
       chain prerouting {
           type nat hook prerouting priority dstnat; policy accept;
           udp dport 443 redirect to :51820
       }
   }
   NFT
   nft -f /etc/wireguard/wireguard_udp_443_redirect.nft
   systemctl enable --now wg-quick@wg0
   echo "solo01_wireguard_public_key=$(wg pubkey < /etc/wireguard/wg0.private_key)"
   echo "solo01_ssh_host_key=$(cut -d ' ' -f 1,2 /etc/ssh/ssh_host_ed25519_key.pub)"
   SET_UP_THE_PRIVATE_CONNECTION
   ```

   Then, from the two printed lines:

   - Write `solo01_wireguard_public_key` into `wireguard_public_key` in
     `inventories/servers.yml`.
   - Add `solo01`'s identity under its WireGuard address to
     `ssh/known_hosts`, below the comment line, as
     `10.20.1.1 ssh-ed25519 AAAA...`.
   - Write the laptop's doubleshell file, which holds no secret:

     ```sh
     # On the laptop. SOLO01_KEY: solo01's WireGuard public key. ADDRESS: its public IPv4 address.
     mkdir -p ~/.config/doubleshell && chmod 700 ~/.config/doubleshell
     cat > ~/.config/doubleshell/solo01.conf <<'DOUBLESHELL'
     [Interface]
     Address = 10.20.10.1/32

     [Peer]
     PublicKey = SOLO01_KEY
     Endpoint = ADDRESS:443
     AllowedIPs = 10.20.1.1/32
     DOUBLESHELL
     ```

     The file has no `PrivateKey` line: doubleshell uses the laptop's key
     from step 1.

   Delete `~/.ssh/solo01_first_connection_known_hosts` once the
   check below passes: nothing uses the public SSH port again.

   **Check:** `ssh -F ssh/config root@10.20.1.1 hostname`, run in the
   systems repository, prints the server's name. This proves SSH through
   WireGuard on UDP 443 works before the next part closes the public SSH
   port. Mark `solo01_is_a_debian_13_server_with_a_public_address` and
   `mmx_and_doubleshell_are_installed_on_the_laptop` `done` in the
   checklist.

   **Keep going without stopping for a reply** into step 4 (or step 5 if
   `gh` creates the private repository automatically); mention that the
   server and WireGuard connection are ready when you next pause for the
   person.

4. **The private repository at GitHub.** With `gh` signed in, create it
   yourself: `gh repo create acme_systems --private --source . --push`.
   Without `gh`, ask the person:

   ```text
   Your private settings repository, at GitHub

   What this is for:  a copy of your server's settings, so a lost laptop loses nothing.

   Do this now:
     1. Open https://github.com/new
     2. Name it acme_systems, choose Private, and add nothing else.
     3. Click "Create repository".

   Then say "done".
   ```

   Add the remote and push with the person's existing Git login.

   **Check:** GitHub lists the repository as private, and `main` is
   pushed. Mark `the_systems_repository_is_private_and_holds_the_template`
   `done` once `mmx validate solo_server` passes at the end of this part.

5. **The vault passphrase.** The vault, `secrets_vault.age`, holds every
   secret, encrypted with one passphrase that only the person chooses.

   **Ask the person:**

   ```text
   Your secrets' passphrase

   What this is for:  one passphrase locks every secret of your server.  Nobody can recover it
   if it is lost, so it goes in your password manager first.

   Do this now:
     1. In 1Password, make a new password item named "acme_systems vault passphrase",
        with a generated password of 30 characters or more.  Save it.
     2. Run this in your own terminal window, and paste the passphrase twice when asked:

   cd ~/code/acme_systems && ~/.local/bin/mmx secrets write-passphrase-file

   Then say "done".
   ```

   The command creates the empty vault and writes the passphrase to
   `~/.config/mmx/vault-passphrase-acme_systems`, readable by the person
   only, outside the repository. Commit `secrets_vault.age`.

   **Check:** `head -n 1 secrets_vault.age` prints
   `age-encryption.org/v1`, and `mmx secrets list-keys` runs without a
   prompt. The person confirmed the passphrase is in the password manager.

6. **Cloudflare.** One command sets up the domain's DNS records, the cache
   rule, the tunnel, the R2 buckets with the backup bucket's 14-day lock,
   R2 keys that each reach one bucket, and the outside alarm Worker. It
   saves every key it makes in the vault and prints a summary with no
   secret in it.

   First, if `dig` is installed on the laptop, check where the domain's
   nameservers point: `dig NS example.org +short`. When `dig` is not
   installed, or when the nameservers do not name Cloudflare, keep the
   registrar line in step 2 of the screen below.

   **Ask the person:**

   ```text
   Your Cloudflare account and key

   What this is for:  Cloudflare is your site's front door, and holds your backups and your outside alarm.
   The free plan is enough.

   Do this now:
     1. Sign in at https://dash.cloudflare.com (or sign up, free).
     2. If example.org is not in your account yet: click "Add a domain", type example.org,
        choose the Free plan, and change the nameservers at your registrar to the two Cloudflare shows.
     3. Open https://dash.cloudflare.com/profile/api-tokens, click "Create Token",
        then "Create Custom Token".  Cloudflare calls the key an API token.
     4. Name it "solo setup", and add these permissions:
          Account  Cloudflare Tunnel     Edit
          Account  Workers R2 Storage    Edit
          Account  Workers Scripts       Edit
          Account  Account API Tokens    Edit
          Account  Account Settings      Read
          Zone     Zone                  Read
          Zone     DNS                   Edit
          Zone     Cache Rules           Edit
        Under Zone Resources, choose example.org.  Create the token and copy it.
     5. Run this in your own terminal window, and paste the key when asked:

   cd ~/code/acme_systems
   export PATH="$HOME/.local/bin:$PATH" MMX_VAULT_PASSWORD_FILE="$HOME/.config/mmx/vault-passphrase-acme_systems"
   set-up-solo-services cloudflare --repo-dir . --domain example.org --apps shop=example.org:acme-shop-uploads --backup-bucket acme-systems-backups --worker-file SKILL_DIRECTORY/worker/solo_outside_alarm_worker.js > cloudflare_summary.json

   Then say "done".  You can delete the key at Cloudflare afterwards: nothing keeps it.
   ```

   Fill the command before you show it: `--apps` gets one
   `NAME=HOST:UPLOADS_BUCKET` item for each app, with the same names as
   `public_host_names` and `production_uploads_r2_bucket`, and
   `NAME=HOST` for an app with no uploads. `--backup-bucket` is a new
   bucket for the backups only. `--worker-file` is the full path of
   `worker/solo_outside_alarm_worker.js` beside the installed skill's
   `SKILL.md`. The command prints its progress in the terminal and writes
   the summary into `cloudflare_summary.json`.

   Then read `cloudflare_summary.json` and fill:

   | Summary field | Setting in `mmx_project_settings.yml` |
   | --- | --- |
   | `account_id` | `cloudflare_account_id` |
   | `r2_s3_endpoint` | `backups.r2_s3_endpoint` (already without `https://`) |
   | `backup_bucket` | `backups.bucket_name` |
   | `lock_days` | `backups.lock_backups_against_deletion_for_days` |
   | `worker_url` | `outside_alarm.worker_url` |

   For an app with uploads, also add the R2 address the app reads to its
   `env`, under the setting name the app's code uses, such as
   `R2_ENDPOINT: https://ACCOUNT_ID.r2.cloudflarestorage.com`. Delete
   `cloudflare_summary.json`, and commit the settings and the
   vault.

   **Check:** `set-up-solo-services secrets list --repo-dir .` lists
   `cloudflare_tunnel_token`, `pgbackrest_r2_access_key_id`,
   `pgbackrest_r2_secret_access_key`, `outside_alarm_check_in_token`, and
   `shop_R2_ACCESS_KEY_ID` and `shop_R2_SECRET_ACCESS_KEY` for each app with
   uploads.

7. **The secrets that need no person.** Make each random secret yourself.
   `secrets generate` makes the value inside MMX and never prints it:

   ```sh
   # On the laptop, in the systems repository, with the two settings of "Running MMX from the laptop".
   set-up-solo-services secrets generate --repo-dir . --key pgbackrest_repository_cipher_passphrase --length 64
   set-up-solo-services secrets generate --repo-dir . --key shop_database_password
   set-up-solo-services secrets generate --repo-dir . --key shop_SECRET_KEY_BASE --length 64
   ```

   Run one `<database_name>_database_password` line for each app's
   database, and one `<app>_<VARIABLE>` line for each
   `generated_by_the_setup` entry in its `secrets_from_vault`, except
   `R2_ACCESS_KEY_ID` and `R2_SECRET_ACCESS_KEY`, which step 6 saved. Use
   `--length 64` for a framework's signing secret.

   **Check:** `set-up-solo-services secrets list --repo-dir .` lists each
   key.

8. **The keys only the person has.** The server needs a read-only key to
   download the app's image from GitHub, and each app may need keys from
   other companies, such as Stripe.

   **Ask the person:**

   ```text
   Your GitHub read-only key

   What this is for:  your server downloads your app's packaged versions from GitHub with it.
   It can only read packages.

   Do this now:
     1. Open https://github.com/settings/tokens/new
        (GitHub calls the key a personal access token, classic.)
     2. Note: "solo01 reads packages".  Expiration: No expiration.  Tick only "read:packages".
     3. Click "Generate token" and copy it.
     4. Run this in your own terminal window, and paste the key when asked:

   cd ~/code/acme_systems
   export PATH="$HOME/.local/bin:$PATH" MMX_VAULT_PASSWORD_FILE="$HOME/.config/mmx/vault-passphrase-acme_systems"
   set-up-solo-services secrets set --repo-dir . --key container_registry_read_only_token --prompt "your GitHub read-only key"

   Then say "done".
   ```

   For each `an_outside_service` entry, add one more
   `set-up-solo-services secrets set --repo-dir . --key shop_STRIPE_SECRET_KEY --prompt "your Stripe secret key"`
   line to the same screen, with where to find the key at that company.

   **Check:** `set-up-solo-services secrets list --repo-dir .` lists each
   key. `mmx secrets usage` shows every key as `vault secrets_vault.age`,
   except the alert keys, which wait for "Alerts". `mmx validate solo_server`
   prints `Valid`. Commit and push. Mark
   `the_vault_exists_and_the_passphrase_is_kept_safe` `done`.

9. **The end of "Your server and Cloudflare".** Do not stop and wait for a
   reply or send a status-only message here: the person has nothing to act on
   until the site is live and alerts are ready to set up. Go straight into
   [Install everything and go live](04_install_everything_and_go_live.md) and
   run `mmx validate solo_server`, `mmx dryrun solo_server`, and
   `mmx run solo_server` in this same turn.

## If something is different

- **The company's server logs in as another user,** such as `debian`, and
  refuses `root` (step 3): run the same command as that user with
  `sudo bash -s --` in place of `bash -s --`. The next part's run uses
  `root`, so first copy the SSH key to root:
  `sudo install -m 600 -D ~/.ssh/authorized_keys /root/.ssh/authorized_keys`.
- **The company has its own firewall in front of the server** (step 3):
  ask the person to allow UDP 443 and UDP 51820 in, then check again.
- **The server is not Debian 13, or is Arm** (step 3): the command stops
  and changes nothing. Ask the person to delete the server and create the
  right kind, in one line.
- **`ssh -F ssh/config root@10.20.1.1 hostname` times out** (step 3): run
  `~/.local/bin/doubleshell --wireguard-config ~/.config/doubleshell/solo01.conf list-machines`.
  A `never` handshake means UDP 443 does not reach the server: check the
  company's firewall and the address in `solo01.conf`.
- **The laptop already has a doubleshell key** (step 1):
  `create-wireguard-key` refuses to replace it. Keep it, and use its public
  key: `wg pubkey < ~/.config/doubleshell/wireguard_private_key` prints it
  where WireGuard tools are installed.
- **The domain is "Pending" at Cloudflare** (step 6): the command works
  anyway. Going live waits until Cloudflare shows the domain as "Active".
- **The person's terminal cannot paste into a hidden prompt** (step 6):
  `--token-file PATH` reads the key from a file of mode `600` that the
  person writes. The person deletes the file afterwards.
- **The command fails with a Cloudflare permission error** (step 6): the
  message names what Cloudflare refused. Ask the person to add that
  permission to the key and run the same command again: a second run finds
  what the first one made.

## Why it works this way

**WireGuard before anything else.** The next part closes the server's
public SSH port. Proving SSH through WireGuard first means the close can
never lock the laptop out.

**The person's terminal for every secret.** A secret typed into the chat is
stored with the conversation at the coding agent's company. The hidden
prompt sends it straight into the encrypted vault.

**Keys that each reach one bucket.** The backup key on `solo01` can write
backups but cannot change the bucket's lock, so a break-in on `solo01`
cannot delete them.

Next: [Install everything and go live](04_install_everything_and_go_live.md).
