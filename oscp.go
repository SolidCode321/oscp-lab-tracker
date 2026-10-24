// oscp - a tiny, fast target tracker for OSCP / lab work.
//
// One static binary. It is both:
//   - a CLI          (oscp add / ls / show / port / cred / note / flag / env / push)
//   - a live server  (oscp serve)  -- leave it running on Kali all session
//
// Data lives in data/hosts.json next to the binary. The static site (index.html)
// reads that file when deployed on Cloudflare; when you run `oscp serve` locally
// the same page becomes live-editable through a small HTTP API.
//
// Build:  go build -o oscp oscp.go
// No dependencies beyond the Go standard library.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Host struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Ports    string `json:"ports"`
	Creds    string `json:"creds"`
	Flag     string `json:"flag"` // none | user | root
	Notes    string `json:"notes"`
}

type Store struct {
	Hosts   []Host `json:"hosts"`
	Updated string `json:"updated"`
}

var (
	root    string
	dataF   string
	currF   string
	confF   string
	conf    map[string]string
	useCol  = isTTY(os.Stdout) && os.Getenv("NO_COLOR") == ""
	useColE = isTTY(os.Stderr) && os.Getenv("NO_COLOR") == ""
)

// loadConf reads oscp.conf (key = value, # comments). Missing file -> defaults.
func loadConf() map[string]string {
	c := map[string]string{
		"addr":   "127.0.0.1:8777",
		"site":   "",
		"domain": "", // fallback domain used by `env` when a host has none
	}
	b, err := os.ReadFile(confF)
	if err != nil {
		return c
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "="); i > 0 {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			if k != "" {
				c[k] = v
			}
		}
	}
	return c
}

func main() {
	exe, _ := os.Executable()
	root = filepath.Dir(exe)
	// When run via `go run`, the binary is in a temp dir; fall back to CWD so data/ resolves.
	if _, err := os.Stat(filepath.Join(root, "data")); err != nil {
		if wd, e := os.Getwd(); e == nil {
			if _, e2 := os.Stat(filepath.Join(wd, "data")); e2 == nil {
				root = wd
			}
		}
	}
	dataF = filepath.Join(root, "data", "hosts.json")
	currF = filepath.Join(root, ".current")
	confF = filepath.Join(root, "oscp.conf")
	conf = loadConf()

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "serve":
		err = cmdServe(args)
	case "add":
		err = cmdAdd(args)
	case "ls", "list":
		err = cmdLs(args)
	case "show":
		err = cmdShow(args)
	case "set":
		err = cmdSet(args)
	case "port":
		err = cmdPort(args)
	case "cred":
		err = cmdCred(args)
	case "note":
		err = cmdNote(args)
	case "flag":
		err = cmdFlag(args)
	case "rm", "del":
		err = cmdRm(args)
	case "env":
		err = cmdEnv(args)
	case "push":
		err = cmdPush(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		die("unknown command %q (run `oscp help`)", cmd)
	}
	if err != nil {
		die("%s", err.Error())
	}
}

// ---------- store ----------

func load() (*Store, error) {
	b, err := os.ReadFile(dataF)
	if errors.Is(err, os.ErrNotExist) {
		return &Store{Hosts: []Host{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("could not parse %s: %v", dataF, err)
	}
	if s.Hosts == nil {
		s.Hosts = []Host{}
	}
	return &s, nil
}

func save(s *Store) error {
	s.Updated = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if err := os.MkdirAll(filepath.Dir(dataF), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := dataF + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dataF)
}

func newID() string {
	b := make([]byte, 3)
	rand.Read(b)
	return "h" + fmt.Sprintf("%x", time.Now().Unix()) + hex.EncodeToString(b)
}

// find returns the single matching host index, or an error listing candidates.
func find(s *Store, q string) (int, error) {
	ql := strings.ToLower(strings.TrimSpace(q))
	var exact, part []int
	for i, h := range s.Hosts {
		if strings.ToLower(h.Name) == ql || strings.ToLower(h.IP) == ql {
			exact = append(exact, i)
		}
		hay := strings.ToLower(h.Name + " " + h.IP + " " + h.Hostname)
		if strings.Contains(hay, ql) {
			part = append(part, i)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0], nil
	case len(exact) > 1:
		return -1, ambig(q, s, exact)
	case len(part) == 1:
		return part[0], nil
	case len(part) == 0:
		return -1, fmt.Errorf("no host matches %q. Run `oscp ls`.", q)
	default:
		return -1, ambig(q, s, part)
	}
}

func ambig(q string, s *Store, idx []int) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches several hosts:\n", q)
	for _, i := range idx {
		fmt.Fprintf(&b, "  %s  %s\n", s.Hosts[i].Name, s.Hosts[i].IP)
	}
	b.WriteString("Be more specific (exact name or IP).")
	return errors.New(b.String())
}

// ---------- commands ----------

func cmdAdd(a []string) error {
	fs := newFlags()
	ip := fs.str("ip")
	host := fs.str("host")
	osn := fs.str("os")
	pos, err := fs.parse(a)
	if err != nil {
		return err
	}
	if len(pos) < 1 {
		return errors.New("usage: oscp add <name> [--ip IP] [--host FQDN] [--os OS]")
	}
	name := strings.TrimSpace(strings.Join(pos, " "))
	s, err := load()
	if err != nil {
		return err
	}
	for _, h := range s.Hosts {
		if strings.EqualFold(h.Name, name) {
			return fmt.Errorf("a host named %q already exists.", name)
		}
	}
	h := Host{ID: newID(), Name: name, IP: *ip, Hostname: *host, OS: *osn, Flag: "none"}
	s.Hosts = append([]Host{h}, s.Hosts...)
	if err := save(s); err != nil {
		return err
	}
	ok("+ added " + bold(name) + sp(*ip))
	return nil
}

func cmdLs(a []string) error {
	s, err := load()
	if err != nil {
		return err
	}
	if len(s.Hosts) == 0 {
		fmt.Println(dim("no hosts yet. add one:  oscp add <name> --ip <ip>"))
		return nil
	}
	wN, wI, wO := 4, 2, 2
	for _, h := range s.Hosts {
		wN = max(wN, len(h.Name))
		wI = max(wI, len(h.IP))
		wO = max(wO, len(h.OS))
	}
	fmt.Println(bold(fmt.Sprintf("%-*s  %-*s  %-*s  %-5s  PORTS", wN, "NAME", wI, "IP", wO, "OS", "FLAG")))
	var r, u int
	for _, h := range s.Hosts {
		if h.Flag == "root" {
			r++
		} else if h.Flag == "user" {
			u++
		}
		fmt.Printf("%-*s  %s  %-*s  %s  %s\n",
			wN, h.Name,
			pad(cyan(h.IP), h.IP, wI),
			wO, h.OS,
			pad(flagDisp(h.Flag), flagLabelShort(h.Flag), 5),
			dim(fmt.Sprintf("%d", portCount(h.Ports))))
	}
	fmt.Println(dim(fmt.Sprintf("\n%d host(s)  -  %d root, %d user", len(s.Hosts), r, u)))
	return nil
}

func cmdShow(a []string) error {
	if len(a) < 1 {
		return errors.New("usage: oscp show <name|ip>")
	}
	s, err := load()
	if err != nil {
		return err
	}
	i, err := find(s, a[0])
	if err != nil {
		return err
	}
	h := s.Hosts[i]
	fmt.Println(bold(h.Name) + "   " + cyan(orDash(h.IP)))
	if h.Hostname != "" {
		fmt.Println(dim("hostname  ") + h.Hostname)
	}
	if h.OS != "" {
		fmt.Println(dim("os        ") + h.OS)
	}
	fmt.Println(dim("flag      ") + flagDisp(h.Flag))
	for _, f := range []struct{ label, val string }{{"ports", h.Ports}, {"creds", h.Creds}, {"notes", h.Notes}} {
		if f.val != "" {
			fmt.Println(dim("\n" + f.label + ":"))
			for _, line := range strings.Split(f.val, "\n") {
				fmt.Println("  " + line)
			}
		}
	}
	return nil
}

func cmdSet(a []string) error {
	if len(a) < 3 {
		return errors.New("usage: oscp set <host> <ip|hostname|os|name> <value>")
	}
	s, err := load()
	if err != nil {
		return err
	}
	i, err := find(s, a[0])
	if err != nil {
		return err
	}
	val := strings.Join(a[2:], " ")
	switch strings.ToLower(a[1]) {
	case "ip":
		s.Hosts[i].IP = val
	case "host", "hostname":
		s.Hosts[i].Hostname = val
	case "os":
		s.Hosts[i].OS = val
	case "name":
		s.Hosts[i].Name = val
	default:
		return errors.New("field must be one of: ip, hostname, os, name")
	}
	if err := save(s); err != nil {
		return err
	}
	ok("updated " + s.Hosts[i].Name + " " + a[1])
	return nil
}

func cmdPort(a []string) error {
	fs := newFlags()
	add := fs.boolFlag("add")
	pos, err := fs.parse(a)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: oscp port <host> \"<ports>\" [--add]")
	}
	s, err := load()
	if err != nil {
		return err
	}
	i, err := find(s, pos[0])
	if err != nil {
		return err
	}
	val := strings.Join(pos[1:], " ")
	if *add && s.Hosts[i].Ports != "" {
		existing := splitList(s.Hosts[i].Ports)
		seen := map[string]bool{}
		for _, p := range existing {
			seen[p] = true
		}
		for _, p := range splitList(val) {
			if !seen[p] {
				existing = append(existing, p)
				seen[p] = true
			}
		}
		s.Hosts[i].Ports = strings.Join(existing, ", ")
	} else {
		s.Hosts[i].Ports = strings.TrimSpace(val)
	}
	if err := save(s); err != nil {
		return err
	}
	ok("ports on " + s.Hosts[i].Name + ": " + s.Hosts[i].Ports)
	return nil
}

func cmdCred(a []string) error {
	if len(a) < 2 {
		return errors.New("usage: oscp cred <host> \"<user : pass>\"")
	}
	s, err := load()
	if err != nil {
		return err
	}
	i, err := find(s, a[0])
	if err != nil {
		return err
	}
	line := strings.TrimSpace(strings.Join(a[1:], " "))
	s.Hosts[i].Creds = appendLine(s.Hosts[i].Creds, line)
	if err := save(s); err != nil {
		return err
	}
	ok("+ cred on " + s.Hosts[i].Name + ": " + line)
	return nil
}

func cmdNote(a []string) error {
	fs := newFlags()
	ts := fs.boolFlag("t", "timestamp")
	pos, err := fs.parse(a)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return errors.New("usage: oscp note <host> \"<text>\" [-t]")
	}
	s, err := load()
	if err != nil {
		return err
	}
	i, err := find(s, pos[0])
	if err != nil {
		return err
	}
	line := strings.TrimSpace(strings.Join(pos[1:], " "))
	if *ts {
		line = "[" + time.Now().Format("15:04") + "] " + line
	}
	s.Hosts[i].Notes = appendLine(s.Hosts[i].Notes, line)
	if err := save(s); err != nil {
		return err
	}
	ok("+ note on " + s.Hosts[i].Name)
	return nil
}

func cmdFlag(a []string) error {
	if len(a) < 2 {
		return errors.New("usage: oscp flag <host> none|user|root")
	}
	v := strings.ToLower(a[1])
	if v != "none" && v != "user" && v != "root" {
		return errors.New("flag must be one of: none, user, root")
	}
	s, err := load()
	if err != nil {
		return err
	}
	i, err := find(s, a[0])
	if err != nil {
		return err
	}
	s.Hosts[i].Flag = v
	if err := save(s); err != nil {
		return err
	}
	ok("flag on " + s.Hosts[i].Name + " = " + flagDisp(v))
	return nil
}

func cmdRm(a []string) error {
	if len(a) < 1 {
		return errors.New("usage: oscp rm <host>")
	}
	s, err := load()
	if err != nil {
		return err
	}
	i, err := find(s, a[0])
	if err != nil {
		return err
	}
	name := s.Hosts[i].Name
	s.Hosts = append(s.Hosts[:i], s.Hosts[i+1:]...)
	if err := save(s); err != nil {
		return err
	}
	fmt.Println(red("- removed ") + name)
	return nil
}

func cmdEnv(a []string) error {
	s, err := load()
	if err != nil {
		return err
	}
	var i int
	if len(a) >= 1 && a[0] != "" {
		if i, err = find(s, a[0]); err != nil {
			return err
		}
		os.WriteFile(currF, []byte(s.Hosts[i].Name), 0o644)
	} else {
		b, e := os.ReadFile(currF)
		if e != nil || strings.TrimSpace(string(b)) == "" {
			return errors.New("no focused target yet. Use:  target <name>   (or: oscp env <name>)")
		}
		if i, err = find(s, strings.TrimSpace(string(b))); err != nil {
			return err
		}
	}
	h := s.Hosts[i]
	user, pass := parseCred(h.Creds)
	domain := h.Hostname
	if domain == "" {
		domain = conf["domain"] // fallback from oscp.conf
	}
	for _, kv := range [][2]string{
		{"ip", h.IP}, {"domain", domain}, {"rhost", h.IP},
		{"box", h.Name}, {"user", user}, {"pass", pass},
	} {
		fmt.Printf("export %s=%s\n", kv[0], shq(kv[1]))
	}
	bits := []string{}
	if h.IP != "" {
		bits = append(bits, "$ip="+h.IP)
	}
	if domain != "" {
		bits = append(bits, "$domain="+domain)
	}
	if user != "" {
		bits = append(bits, "$user="+user)
	}
	einfo("focused " + h.Name + ifStr(len(bits) > 0, "  "+strings.Join(bits, "  "), ""))
	return nil
}

func cmdPush(a []string) error {
	s, _ := load()
	msg := strings.Join(a, " ")
	if msg == "" {
		msg = fmt.Sprintf("update targets (%d hosts) %s", len(s.Hosts), time.Now().Format("2006-01-02 15:04"))
	}
	if err := git("add", "data/hosts.json"); err != nil {
		return err
	}
	// nothing staged?
	if exec.Command("git", "-C", root, "diff", "--cached", "--quiet").Run() == nil {
		fmt.Println(dim("nothing to push - data/hosts.json unchanged."))
		return nil
	}
	if err := git("commit", "-m", msg); err != nil {
		return err
	}
	fmt.Println(dim("pushing..."))
	if err := git("push"); err != nil {
		return err
	}
	msg2 := "pushed. Cloudflare Pages will redeploy in ~30s."
	if conf["site"] != "" {
		msg2 += " -> " + conf["site"]
	}
	ok(msg2)
	return nil
}

func git(args ...string) error {
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("git %s failed (is this a git repo with a remote set?)", strings.Join(args, " "))
	}
	return nil
}

// ---------- server ----------

func cmdServe(a []string) error {
	fs := newFlags()
	addr := fs.str("addr")
	if _, err := fs.parse(a); err != nil {
		return err
	}
	listen := *addr
	if listen == "" {
		listen = conf["addr"]
	}
	if listen == "" {
		listen = "127.0.0.1:8777"
	}

	mux := http.NewServeMux()

	// API: GET returns the store; PUT replaces it (used by the live web UI).
	mux.HandleFunc("/api/hosts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		switch r.Method {
		case http.MethodGet:
			s, err := load()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			writeJSON(w, s)
		case http.MethodPut:
			var s Store
			if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
				http.Error(w, "bad json", 400)
				return
			}
			if s.Hosts == nil {
				s.Hosts = []Host{}
			}
			for k := range s.Hosts {
				if s.Hosts[k].ID == "" {
					s.Hosts[k].ID = newID()
				}
				if s.Hosts[k].Flag == "" {
					s.Hosts[k].Flag = "none"
				}
			}
			if err := save(&s); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			writeJSON(w, map[string]any{"ok": true, "count": len(s.Hosts)})
		default:
			http.Error(w, "method not allowed", 405)
		}
	})

	// Allowlist: the page and the data file only. Everything else (binary,
	// source, config, dotfiles) stays private even though it sits in the repo dir.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/" || r.URL.Path == "/index.html":
			http.ServeFile(w, r, filepath.Join(root, "index.html"))
		case r.URL.Path == "/data/hosts.json":
			w.Header().Set("Cache-Control", "no-store")
			http.ServeFile(w, r, dataF)
		default:
			http.NotFound(w, r)
		}
	})

	fmt.Fprintln(os.Stderr, green("oscp serving ")+"on "+bold("http://"+listen)+
		dim("   (data: "+dataF+")"))
	fmt.Fprintln(os.Stderr, dim("leave this running; edit in the browser or with `oscp` in another shell. Ctrl-C to stop."))
	srv := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	return srv.ListenAndServe()
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ---------- helpers ----------

func parseCred(creds string) (string, string) {
	for _, line := range strings.Split(creds, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i := strings.Index(line, " : "); i >= 0 {
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+3:])
		}
		if i := strings.Index(line, ":"); i >= 0 {
			return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:])
		}
		return line, ""
	}
	return "", ""
}

func shq(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'" }

func splitList(s string) []string {
	s = strings.ReplaceAll(s, "\n", ",")
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func portCount(s string) int { return len(splitList(s)) }

func appendLine(cur, line string) string {
	cur = strings.TrimRight(cur, "\n")
	if cur == "" {
		return line
	}
	return cur + "\n" + line
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
func sp(s string) string {
	if s == "" {
		return ""
	}
	return "  " + s
}
func ifStr(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// pad a possibly-coloured string to visible width n (col holds the plain text).
func pad(colored, plain string, n int) string {
	if gap := n - len(plain); gap > 0 {
		return colored + strings.Repeat(" ", gap)
	}
	return colored
}

func flagLabelShort(f string) string {
	switch f {
	case "root":
		return "root"
	case "user":
		return "user"
	default:
		return "none"
	}
}

// ---------- tiny flag parser (so we don't fight the stdlib flag pkg over positionals) ----------

type flags struct {
	strs  map[string]*string
	bools map[string]*bool
	alias map[string]string
}

func newFlags() *flags {
	return &flags{strs: map[string]*string{}, bools: map[string]*bool{}, alias: map[string]string{}}
}
func (f *flags) str(name string) *string {
	v := new(string)
	f.strs[name] = v
	return v
}
func (f *flags) boolFlag(names ...string) *bool {
	v := new(bool)
	f.bools[names[0]] = v
	for _, n := range names[1:] {
		f.alias[n] = names[0]
	}
	return v
}

func (f *flags) parse(args []string) ([]string, error) {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			a = a[2:]
		} else if strings.HasPrefix(a, "-") && len(a) > 1 {
			a = a[1:]
		} else {
			pos = append(pos, args[i])
			continue
		}
		if real, ok := f.alias[a]; ok {
			a = real
		}
		if p, ok := f.bools[a]; ok {
			*p = true
			continue
		}
		if p, ok := f.strs[a]; ok {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("--%s needs a value", a)
			}
			i++
			*p = args[i]
			continue
		}
		return nil, fmt.Errorf("unknown option --%s", a)
	}
	return pos, nil
}

// ---------- output / colour ----------

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}
func col(code, s string, on bool) string {
	if !on {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}
func dim(s string) string   { return col("2", s, useCol) }
func bold(s string) string  { return col("1", s, useCol) }
func green(s string) string { return col("32", s, useCol) }
func cyan(s string) string  { return col("36", s, useCol) }
func red(s string) string   { return col("31", s, useCol) }
func flagDisp(f string) string {
	switch f {
	case "root":
		return col("32", "root", useCol)
	case "user":
		return col("33", "user", useCol)
	default:
		return dim("none")
	}
}
func ok(msg string) { fmt.Println(msg) }
func einfo(msg string) {
	arrow := ">"
	if useColE {
		arrow = "\033[36m▶\033[0m"
	}
	fmt.Fprintln(os.Stderr, arrow+" "+msg)
}
func die(format string, args ...any) {
	fmt.Fprintln(os.Stderr, col("31", "error: ", useColE)+fmt.Sprintf(format, args...))
	os.Exit(1)
}

func usage() {
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	fmt.Fprint(w, `oscp - OSCP / lab target tracker

usage: oscp <command> [args]

  serve [--addr host:port]   run the live web UI + API (default 127.0.0.1:8777)
  add <name> [--ip --host --os]   add a target
  ls                         list all hosts
  show <host>                full detail for one host
  set <host> <field> <val>   set ip | hostname | os | name
  port <host> "<ports>"      set ports  (--add to append)
  cred <host> "<u : p>"      append a credential line
  note <host> "<text>"       append a note  (-t prefixes the time)
  flag <host> none|user|root set flag captured
  rm <host>                  delete a host
  env [host]                 print shell exports; omit host to reuse focused one
  push ["msg"]               git commit + push data/hosts.json

<host> matches an exact name/IP first, then a partial. Set NO_COLOR=1 to disable colour.
`)
}
