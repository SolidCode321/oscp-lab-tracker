# OSCP Lab Tracker

A fast, single-binary target tracker for OSCP / HTB / lab work. One JSON file
holds every host (IP, hostname, OS, ports, creds, flag, notes). You update it
with a tiny CLI, keep a live dashboard open in the browser, and pull `$ip` /
`$domain` straight into your shell while you work. Host the read-only view free
on **Cloudflare Pages** from **GitHub**.

No runtime, no dependencies — just one Go binary you can leave running on Kali.

```
oscp-lab-tracker/
├── oscp.go             # the whole tool (CLI + live server) — one Go file
├── index.html          # the web dashboard (served live, or static on Cloudflare)
├── data/hosts.json     # your data — the single source of truth
├── oscp.conf.example   # config template → copy to oscp.conf and edit
├── shell.sh            # `target <box>` → sets $ip $domain $user $pass …
├── _headers            # tells Cloudflare not to cache the data file
├── .gitignore
└── README.md
```

How the parts fit together:

```
        oscp (CLI) ──writes──▶ data/hosts.json ◀──reads── oscp serve ──▶ browser (live)
            │                        │
   target <box> ─exports─▶ $ip $domain …      └──committed by── oscp push ─▶ Cloudflare Pages
```

---

## 1. Build it (once)

You need Go (1.20+). On Kali:

```bash
sudo apt update && sudo apt install -y golang-go      # if you don't have it
cd oscp-lab-tracker
go build -o oscp oscp.go
```

That produces a single `oscp` binary. A prebuilt Linux/amd64 `oscp` is included in
the download for convenience — if it runs (`./oscp help`), you can skip the build;
if not (wrong arch, or you'd rather not trust a prebuilt), just build it as above.

> The binary and your personal `oscp.conf` are gitignored, so they never get pushed.

---

## 2. Configure it

```bash
cp oscp.conf.example oscp.conf
```

`oscp.conf` is plain `key = value` (lines starting `#` are comments):

```ini
# Address `oscp serve` listens on.
addr = 127.0.0.1:8777

# Your deployed site URL — printed after `oscp push` as a reminder.
site = https://oscp-lab-tracker.pages.dev

# Fallback domain for `target` when a host has no hostname of its own.
# Set this once for a single-AD lab; change this one line when the domain changes.
domain =
```

It's optional — without it the defaults above apply.

---

## 3. The CLI

Run from anywhere once you've set up the shell integration (step 5); otherwise
`./oscp <command>` from the repo folder.

| Command | What it does |
|---|---|
| `oscp add <name> [--ip IP] [--host FQDN] [--os OS]` | Add a target |
| `oscp ls` | List all hosts as a table |
| `oscp show <name\|ip>` | Full detail for one host |
| `oscp set <host> <field> <value>` | Set `ip` / `hostname` / `os` / `name` |
| `oscp port <host> "<ports>"` | Set the ports string (`--add` to append) |
| `oscp cred <host> "<user : pass>"` | Append a credential line |
| `oscp note <host> "<text>"` | Append a note (`-t` prefixes the time) |
| `oscp flag <host> none\|user\|root` | Mark flag captured |
| `oscp rm <host>` | Delete a host |
| `oscp env [host]` | Print shell exports (used by `target`) |
| `oscp serve [--addr host:port]` | Run the live dashboard + API |
| `oscp push ["msg"]` | `git commit` + `push` the data file |

`<host>` matches an exact name or IP first, then a partial on name/hostname/IP;
if it's ambiguous it lists the candidates. Colour output turns off with `NO_COLOR=1`.

A real session:

```bash
oscp add Sauna --ip 10.10.11.42 --os Windows
oscp port Sauna "53, 88, 135, 389, 445, 5985"
oscp port Sauna "3268, 3269" --add
oscp cred Sauna "svc_backup : Password123!"
oscp note Sauna -t "AS-REP roast on svc_backup, cracked offline"
oscp flag Sauna user
oscp show Sauna
oscp push "sauna: user flag"
```

---

## 4. The live dashboard

Leave the server running all session:

```bash
./oscp serve
# oscp serving on http://127.0.0.1:8777
```

Open `http://127.0.0.1:8777`. The page shows a green **live** badge and
**auto-refreshes every few seconds**, so as you run `oscp` commands in another
terminal the dashboard updates itself — no reload. Each host card has **Copy
exports** (the `export ip=…` block) and **Copy summary** buttons, a filter box,
and a light/dark toggle.

When the same `index.html` is served by Cloudflare (no API behind it) the page
falls back to a read-only **static** view of `data/hosts.json`.

---

## 5. `$ip` and `$domain` in your lab shell

A program can't set variables in the shell that launched it, so this uses a small
shell function. Add two lines to your `~/.bashrc` or `~/.zshrc`:

```bash
export OSCP_DIR="$HOME/oscp-lab-tracker"     # wherever you put the repo
source "$OSCP_DIR/shell.sh"
```

Open a new terminal, then:

```bash
target sauna                 # focus a box
echo $ip                     # 10.10.11.42
nmap -sCV -p- $ip
smbclient -L //$ip -U "$user%$pass"
curl http://$domain/
feroxbuster -u http://$ip/

target                       # with no name: reloads the last focused box
```

`target <box>` sets, in your current shell:

| var | from |
|---|---|
| `$ip`, `$rhost` | the host's IP |
| `$domain` | its hostname (or the `domain` in `oscp.conf` if it has none) |
| `$box` | the host's name |
| `$user`, `$pass` | parsed from its first credential line |

It also defines `oscp` (so the CLI works anywhere) and tab-completion for `target`.

---

## 6. Leave it running on Kali

Keep the server up across terminals. Two easy ways:

**tmux** (simplest):

```bash
tmux new -d -s oscp "$HOME/oscp-lab-tracker/oscp serve"
# reattach anytime:  tmux attach -t oscp
```

**systemd user service** (starts on login, restarts if it dies):

```bash
mkdir -p ~/.config/systemd/user
cat > ~/.config/systemd/user/oscp.service <<EOF
[Unit]
Description=OSCP Lab Tracker
[Service]
ExecStart=%h/oscp-lab-tracker/oscp serve
Restart=always
[Install]
WantedBy=default.target
EOF
systemctl --user daemon-reload
systemctl --user enable --now oscp
# logs:  journalctl --user -u oscp -f
```

Keep `addr` on `127.0.0.1` so the dashboard (which shows credentials) is reachable
only from your own machine.

---

## 7. Host the read-only site on GitHub + Cloudflare Pages (free)

### a. Push to GitHub — make the repo **Private** (it holds creds)

```bash
cd oscp-lab-tracker
git init
git add .
git commit -m "initial commit"
git branch -M main
git remote add origin git@github.com:<you>/oscp-lab-tracker.git
git push -u origin main
```

### b. Connect Cloudflare Pages

1. Cloudflare dashboard → **Workers & Pages** → **Create** → **Pages** → **Connect to Git**.
2. Pick the `oscp-lab-tracker` repo.
3. Build settings — it's a plain static site, so:
   - **Framework preset:** `None`
   - **Build command:** *(leave empty)*
   - **Build output directory:** `/`
4. **Save and Deploy.** You get a URL like `oscp-lab-tracker.pages.dev` — put that in `oscp.conf` as `site`.

From then on every `git push` (including `oscp push`) redeploys in ~30s.

### c. Lock it down with Cloudflare Access

A `.pages.dev` URL is public to anyone with the link, and yours holds credentials.
Gate it (free tier):

- Pages project → **Settings** → **Access policy** → enable, and add a policy allowing
  only your own email. The site then requires a one-time login before it loads.

That gives you a private, login-gated, synced copy of your notebook with no auth code.

> On the hosted copy, `index.html`, `oscp.go`, and the README are also fetchable
> behind the Access gate — that's fine (your own tool, no secrets). The secret is
> `data/hosts.json`; Access is what protects it. Your local `oscp.conf` is
> gitignored and never deployed.

---

## 8. When a domain changes

- **One box changed:** `oscp set sauna hostname dc02.megabank.local` — then
  `target sauna` re-exports the new `$domain`.
- **Whole-lab default:** edit the `domain =` line in `oscp.conf`. Every host without
  its own hostname picks it up on the next `target`.

---

## 9. Data format

`data/hosts.json`:

```json
{
  "hosts": [
    {
      "id": "h...",
      "name": "Sauna",
      "ip": "10.10.11.42",
      "hostname": "dc01.megabank.local",
      "os": "Windows",
      "ports": "53, 88, 135, 389, 445, 5985",
      "creds": "svc_backup : Password123!",
      "flag": "user",
      "notes": "..."
    }
  ],
  "updated": "2026-10-24T12:00:00Z"
}
```

`ports`, `creds`, and `notes` are free-text strings (newlines allowed), so you can
paste raw nmap output or scribble whatever you like. The CLI, the server, and the
site all treat them the same way.
