# OSCP Lab Tracker

A fast, single-binary target tracker for OSCP / HTB / lab work. One JSON file holds
every host — IP, hostname, OS, ports, creds, flag, notes. You update it with a tiny
CLI, pull `$ip` / `$domain` straight into your shell, keep a live dashboard open in the
browser, and (optionally) host a read-only mirror free on Cloudflare Pages from GitHub.

No runtime, no dependencies — just one Go binary you can leave running on Kali.

---

## Contents

1. [What's in the box](#1-whats-in-the-box)
2. [Quick setup (one command)](#2-quick-setup-one-command)
3. [Manual setup](#3-manual-setup)
4. [The config file](#4-the-config-file)
5. [CLI reference](#5-cli-reference)
6. [`$ip` / `$domain` in your shell](#6-ip--domain-in-your-shell)
7. [The live dashboard](#7-the-live-dashboard)
8. [A full lab session, step by step](#8-a-full-lab-session-step-by-step)
9. [Handling multiple IPs](#9-handling-multiple-ips)
10. [Finishing a lab (archive / clear)](#10-finishing-a-lab-archive--clear)
11. [Hosting the mirror on GitHub + Cloudflare](#11-hosting-the-mirror-on-github--cloudflare)
12. [Data, sync and git](#12-data-sync-and-git)
13. [Running as root (and file permissions)](#13-running-as-root-and-file-permissions)
14. [Troubleshooting](#14-troubleshooting)

---

## 1. What's in the box

```
oscp-lab-tracker/
├── setup.sh            # one-command first-time setup
├── oscp.go             # the whole tool (CLI + live server), one Go file
├── index.html          # the web dashboard (served live, or static on Cloudflare)
├── data/hosts.json     # your data — the single source of truth (starts empty)
├── oscp.conf.example   # config template → copied to oscp.conf by setup
├── shell.sh            # defines  oscp, target, oscp-up/down/log
├── wrangler.jsonc      # Cloudflare static-hosting config
├── _headers            # stops Cloudflare caching the data file
└── README.md
```

How the parts talk to each other:

```
   oscp (CLI) ──writes──▶ data/hosts.json ◀──reads── oscp serve ──▶ browser dashboard (live)
       │                       │
 target <box> ─▶ $ip $domain   └─ oscp push ─▶ GitHub ─▶ Cloudflare (read-only mirror)
```

---

## 2. Quick setup (one command)

Clone, then run the installer:

```bash
git clone https://github.com/<you>/oscp-lab-tracker.git
cd oscp-lab-tracker
chmod +x setup.sh
./setup.sh
source ~/.zshrc        # or ~/.bashrc — setup tells you which
```

`setup.sh` installs Go if needed (asks first), builds the `oscp` binary, creates your
`oscp.conf`, and wires `oscp` / `target` / `oscp-up` into your shell. That's it.

> **Cloning a private repo** needs auth, not your GitHub password. Either use a
> Personal Access Token as the password, or set up SSH and clone with
> `git@github.com:<you>/oscp-lab-tracker.git`. See [section 11](#11-hosting-the-mirror-on-github--cloudflare).

---

## 3. Manual setup

If you'd rather not run the script:

```bash
# 1. Build (install Go first if needed: sudo apt install -y golang-go)
go build -o oscp oscp.go

# 2. Config
cp oscp.conf.example oscp.conf

# 3. Shell integration — gives you oscp, target, oscp-up/down/log everywhere
echo "export OSCP_DIR=$PWD" >> ~/.zshrc
echo 'source "$OSCP_DIR/shell.sh"' >> ~/.zshrc
source ~/.zshrc

# 4. (for the background dashboard) make sure tmux is installed
sudo apt install -y tmux
```

Use `~/.bashrc` instead of `~/.zshrc` if you're on bash.

---

## 4. The config file

`oscp.conf` is plain `key = value` (lines starting `#` are comments):

```ini
# Address the dashboard server listens on.
addr = 127.0.0.1:8777

# Your deployed site URL — printed after `oscp push` as a reminder. Optional.
site = https://oscp.alexsecurity.online

# Fallback domain for `target` when a host has no hostname of its own.
# Set it once for a single-AD lab; change this one line when the domain changes.
domain =
```

It's optional — without it the defaults above apply.

---

## 5. CLI reference

Run from anywhere once the shell integration is set up; otherwise `./oscp <command>`.

| Command | What it does |
|---|---|
| `oscp add <name> [--ip IP] [--host FQDN] [--os OS]` | Add a target |
| `oscp ls` | List all hosts as a table |
| `oscp show <name\|ip>` | Full detail for one host |
| `oscp set <host> <field> <value>` | Set `ip` / `hostname` / `os` / `name` |
| `oscp port <host> "<ports>"` | Set the ports string (`--add` to append new ones) |
| `oscp cred <host> "<user : pass>"` | Append a credential line |
| `oscp note <host> "<text>"` | Append a note (`-t` prefixes the time) |
| `oscp flag <host> none\|user\|root` | Mark flag captured |
| `oscp rm <host>` | Delete one host |
| `oscp archive [name] [-y]` | Save an HTML snapshot of all hosts, then clear the tracker |
| `oscp clear [-y]` | Wipe all hosts (no snapshot) |
| `oscp env [host]` | Print shell exports (used by `target`) |
| `oscp serve [--addr host:port]` | Run the live dashboard + API |
| `oscp push ["msg"]` | `git commit` + `push` the data file |

- `<host>` matches an exact name/IP first, then a partial on name/hostname/IP; if it's
  ambiguous it lists the candidates so you can be specific.
- Colour output turns off with `NO_COLOR=1`.
- `port` replaces the whole string by default; `--add` merges in new ports without dupes.
- `cred` and `note` always append a line, building up a list.

---

## 6. `$ip` / `$domain` in your shell

A program can't set variables in the shell that launched it, so this uses the `target`
function (defined in `shell.sh`).

```bash
target forest          # focus a box
echo $ip               # 10.10.10.161
nmap -sCV -p- $ip
smbclient -L //$ip -U "$user%$pass"
curl http://$domain/
target                 # no name → reload the last focused box (handy in a new terminal)
```

`target <box>` sets, in your current shell:

| variable | value |
|---|---|
| `$ip`, `$rhost` | the host's IP |
| `$domain` | its hostname (or the `domain` from `oscp.conf` if it has none) |
| `$box` | the host's name |
| `$user`, `$pass` | parsed from its **first** credential line |

Tab-completion for `target` is set up too — type `target ` and press Tab.

---

## 7. The live dashboard

Start it in the background whenever you want it:

```bash
oscp-up        # starts the server → http://127.0.0.1:8777
oscp-down      # stop it
oscp-log       # watch its output (Ctrl-B then D to leave it running)
```

Open `http://127.0.0.1:8777`. It shows a green **live** badge and **auto-refreshes**, so
as you run `oscp` commands in another terminal the board updates itself — no reload. Each
card has **Copy exports** and **Copy summary**; the toolbar has a filter, a theme toggle,
and two red buttons in live mode:

- **Archive & clear** — download an HTML snapshot of everything, then empty the tracker.
- **Clear** — wipe all hosts, no snapshot (two-click confirm).

Stopping the server never touches your data — it's all in `data/hosts.json` on disk.
`oscp-down` then `oscp-up` brings everything back exactly as it was.

**Live vs static:** when the page is served by `oscp serve` it's live (editable actions,
auto-refresh). The copy hosted on Cloudflare has no backend, so it's always a read-only
**static** mirror — that's expected, and why the Archive/Clear buttons only appear locally.

---

## 8. A full lab session, step by step

```bash
oscp-up                                   # (optional) open the dashboard on a 2nd monitor

# get a box
oscp add Forest --ip 10.10.10.161 --os Windows
target forest                             # $ip now = 10.10.10.161

# recon
nmap -sCV -p- $ip
oscp port forest "53,88,135,139,389,445,464,593,636,3268,5985,9389"
oscp port forest "47001,49664" --add      # append more found later
oscp set forest hostname forest.htb.local # feeds $domain

# as you work
oscp cred forest "svc-alfresco : s3rvice" # first cred line fills $user/$pass
oscp note forest -t "AS-REP roast svc-alfresco, cracked"
oscp note forest -t "WriteDACL via Exchange group -> DCSync"
oscp flag forest user                     # local.txt
oscp flag forest root                     # proof.txt

# move around
oscp ls                                   # overview of every box + flags
oscp show forest                          # full detail of one
target sauna                              # switch focus to another box

# if an IP changes (DHCP / reset)
oscp set forest ip 10.10.10.200
```

---

## 9. Handling multiple IPs

**Many machines (the normal case).** Each machine is its own host — just `oscp add` each.
No limit; `oscp ls` lists them all and `target <name>` jumps between them. `$ip` always
points at whichever box you last focused.

```bash
oscp add Sauna --ip 10.10.10.175
oscp add Forest --ip 10.10.10.161
target sauna      # $ip = 10.10.10.175
target forest     # $ip = 10.10.10.161
```

**One machine with several IPs** (dual-homed, or after you pivot and it has an internal
address too). A host holds one `ip` — the one you attack over your VPN. For the extra:

- Record it in notes:
  ```bash
  oscp note forest "internal NIC: 172.16.1.5 (via pivot)"
  ```
- Or, if you'll work heavily on the internal IP and want `$ip` to give it to you directly,
  make a second entry for that interface:
  ```bash
  oscp add Forest-int --ip 172.16.1.5 --os Windows
  oscp note forest-int "internal face of Forest (10.10.10.161), via chisel"
  target forest-int      # $ip = 172.16.1.5
  ```

Rule of thumb: **one host = one machine, one IP in the `ip` field.** Extra IPs live in
notes; promote one to its own entry only when you need `$ip` to produce it on demand.

---

## 10. Finishing a lab (archive / clear)

```bash
oscp archive "exam-attempt-1"    # -> archive/exam-attempt-1-<date>.html, then empties the tracker
oscp clear                       # just wipe, no snapshot
```

`archive` writes a self-contained HTML report of every host (IPs, ports, creds, notes) to
`archive/`, opens offline in any browser, then clears the tracker so the next lab starts
clean. Both ask for confirmation; pass `-y` to skip it. The same two actions are the red
buttons on the live dashboard.

---

## 11. Hosting the mirror on GitHub + Cloudflare

This is optional — the tool works fully offline. Hosting gives you a read-only copy of
your notebook reachable from anywhere.

### a. Push to GitHub — make the repo **Private** (it holds credentials)

```bash
git init
git add -A
git commit -m "initial commit"
git branch -M main
git remote add origin https://github.com/<you>/oscp-lab-tracker.git
git push -u origin main
```

**Auth:** GitHub no longer accepts your password for git. Use one of:

- **Personal Access Token** — GitHub → Settings → Developer settings → Personal access
  tokens → Fine-grained → repo `oscp-lab-tracker`, Contents: Read and write. Paste the
  token when git asks for the *password*. Cache it with `git config --global credential.helper store`.
- **SSH** — `ssh-keygen -t ed25519`, add `~/.ssh/id_ed25519.pub` at GitHub → SSH keys,
  and use the `git@github.com:<you>/…` remote. No tokens to manage.

### b. Deploy on Cloudflare

Workers & Pages → **Create** → **Pages** → **Connect to Git** → pick the repo.
Build command: empty (or `echo ok` if it insists); **Build output directory:** `/`.
Deploy. If the dashboard only gives you the Workers "Import a repository" flow (which
demands a deploy command), the included `wrangler.jsonc` makes it work — set
**Deploy command:** `npx wrangler deploy`.

Add a custom domain under the project's **Custom domains** (e.g. `oscp.alexsecurity.online`).

### c. Lock it down — required

The site serves `data/hosts.json` (your creds) and the URL is public. Gate it with
**Cloudflare Access** (Zero Trust, free): project → Settings → Access policy → allow only
your own email. The site then needs a one-time login before it loads.

From then on, every `git push` (or `oscp push`) redeploys the mirror in ~30s.

---

## 12. Data, sync and git

`data/hosts.json` is your single source of truth and it's tracked in git (so the hosted
mirror can read it). That has one consequence worth knowing:

- Your live hosts show up as git changes. A `git pull` on a machine that has local edits
  will refuse to overwrite them.
- If you use more than one machine (say a Mac for the repo and Kali for the labs), pick
  **one** as your working copy. Edit there, `oscp push`, and `git pull` on the other
  before you start — or just do all your lab work on the one box.

`data/hosts.json` starts **empty**, so a fresh clone or a cleared tracker opens blank,
ready for your first `oscp add`.

### Data format

```json
{
  "hosts": [
    {
      "id": "h...",
      "name": "Forest",
      "ip": "10.10.10.161",
      "hostname": "forest.htb.local",
      "os": "Windows",
      "ports": "53, 88, 389, 445, 5985",
      "creds": "svc-alfresco : s3rvice",
      "flag": "root",
      "notes": "..."
    }
  ],
  "updated": "2026-10-24T12:00:00Z"
}
```

`ports`, `creds` and `notes` are free-text strings (newlines allowed), so you can paste
raw nmap output or scribble whatever you like. The CLI, the server and the site all treat
them the same way.

---

## 13. Running as root (and file permissions)

`oscp` **never needs root** — it only reads/writes files in this folder and serves on
localhost. The tools you point it at (a SYN scan, etc.) may need root, but oscp itself
doesn't.

**Recommended:** run `oscp` / `target` as your normal user, and `sudo` only the individual
tools that need it. Your variables still work, because your shell expands them *before*
`sudo` runs:

```bash
target forest
sudo nmap -sCV -p- $ip        # $ip is substituted by your shell, then sudo runs nmap
```

**Avoid `sudo su` then oscp.** Two things break:

- In a root shell `~` becomes `/root`, and your shell functions aren't loaded there, so
  `oscp` / `target` come back as *command not found*.
- If you *do* run oscp as root, it writes `data/hosts.json`, `.current`, `archive/` and git
  objects **owned by root**. Switch back to your normal user and you'll hit
  *permission denied* / git *dubious ownership*, because your user can't write root-owned
  files.

If you'd rather live in a root shell, load the tool there and **stay consistent** — never
half root, half user:

```bash
export OSCP_DIR=/home/kali/oscp-lab-tracker
source "$OSCP_DIR/shell.sh"
```

Fix a mixed-ownership mess at any time with:

```bash
sudo chown -R kali:kali ~/oscp-lab-tracker
```

---

## 14. Troubleshooting

**`oscp: command not found`** — your shell hasn't loaded `shell.sh`. Run `source ~/.zshrc`
(or open a new terminal). If you're in a root shell, see [section 13](#13-running-as-root-and-file-permissions).

**It can't find the folder / uses the wrong directory** — `OSCP_DIR` points at the wrong
place (e.g. after moving the repo). Point it at the real folder and reload:

```bash
cd /path/to/oscp-lab-tracker
sed -i "s|^export OSCP_DIR=.*|export OSCP_DIR=\"$PWD\"|" ~/.zshrc
source ~/.zshrc
```

Locate the repo with: `find ~ -type d -name oscp-lab-tracker 2>/dev/null`

**Changes don't show after `git pull`** — the old binary is still running. Rebuild and
restart:

```bash
go build -o oscp oscp.go
oscp-down; oscp-up
```

**`git pull` refuses — "local changes to data/hosts.json"** — you edited hosts on this
machine. Keep them (`oscp push`) or discard and pull:

```bash
git checkout -- data/hosts.json && git pull
```

**git: "A lock file already exists" / `index.lock`** — a git process didn't clean up.
Remove the stale lock:

```bash
rm -f .git/index.lock
```

**"permission denied" writing data, or git "dubious ownership"** — files got created as
root. Fix ownership ([section 13](#13-running-as-root-and-file-permissions)):

```bash
sudo chown -R kali:kali ~/oscp-lab-tracker
```

**Dashboard shows STATIC on `127.0.0.1:8777`** — hard-refresh with Ctrl+Shift+R. If it's
still static, the server isn't running (`oscp-up`), or you're actually on the Cloudflare
URL (which is always static by design — the live badge only appears on localhost).

**`oscp serve`: "address already in use"** — a server is already running. `oscp-down`
first, or change `addr` in `oscp.conf`.

**Cloning a private repo fails with a password prompt** — GitHub dropped password auth for
git. Use a Personal Access Token as the password, or SSH — see
[section 11](#11-hosting-the-mirror-on-github--cloudflare).

**`go: command not found`** — install Go, then rebuild:

```bash
sudo apt install -y golang-go     # or https://go.dev/dl/
go build -o oscp oscp.go
```
