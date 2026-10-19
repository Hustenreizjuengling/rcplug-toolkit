// Command rcplug-setup installs, updates, diagnoses and removes rcplug on a
// RØDECaster over SSH. Double-click it (Windows .exe, macOS, Linux) for a
// menu, or give a command:
//
//	rcplug-setup --host 192.168.1.50 install
//	rcplug-setup --host 192.168.1.50 diagnostics
//	rcplug-setup --host 192.168.1.50 plugin ./my-plugin
//	rcplug-setup --host 192.168.1.50 uninstall
//	rcplug-setup --host 192.168.1.50 keys
//
// It uses the computer's own OpenSSH client (built into Windows 10/11, macOS
// and Linux) and runs rcplug-remote.sh (embedded) on the device for every
// step, like rcplug-wizard.sh and rcplug-setup.cmd do.
package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Hustenreizjuengling/rcplug-toolkit/installer"
	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwimage"
	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/rcdl"
)

var version = "dev"

func main() { os.Exit(run()) }

// run returns the exit status, so that deferred cleanup (the SSH connection
// and its control folder) happens before the program exits.
func run() int {
	host := flag.String("host", "", "IP address or name of the device")
	user := flag.String("user", "root", "SSH user")
	key := flag.String("key", "", "SSH private key")
	payload := flag.String("payload", "", "release tarball rcplug-<version>-aarch64.tar.gz (default: the newest next to this program)")
	yes := flag.Bool("yes", false, "do not ask for confirmation")
	langFlag := flag.String("lang", "", "de or en (default: the system language)")
	noGUI := flag.Bool("no-gui", false, "use the text menu instead of the browser GUI")
	showVer := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: rcplug-setup [options] [gui | menu | install | diagnostics | plugin DIR | stop | start | resetaccess | uninstall | keys | status]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVer {
		fmt.Println(version)
		return 0
	}
	setLang(*langFlag)
	args := flag.Args()
	// Default to the browser GUI; "menu" or --no-gui use the text menu.
	if (len(args) == 0 && !*noGUI) || (len(args) == 1 && args[0] == "gui") {
		if err := runGUI(version, guiDefaults{Host: *host, User: *user, Key: *key, Payload: *payload}); err != nil {
			fmt.Fprintln(os.Stderr, tr("error")+" "+err.Error())
			return 1
		}
		return 0
	}
	if len(args) == 1 && args[0] == "menu" {
		args = nil
	}
	ui := &ui{in: bufio.NewReader(os.Stdin), yes: *yes}
	interactive := len(args) == 0
	if interactive {
		defer ui.waitBeforeExit() // a double-clicked window must not vanish
	}
	fmt.Printf("\n  === rcplug setup %s ===\n\n", version)

	if *host == "" {
		*host = ui.ask(tr("host"))
	}
	if *host == "" {
		return 2
	}
	s, err := newSession(*user, *host, *key)
	if err != nil {
		ui.fail(err)
		return 1
	}
	defer s.close()
	fmt.Println(tr("checking"))
	if err := s.probe(); err != nil {
		ui.fail(err)
		return 1
	}
	fmt.Println(tr("sshOK"))
	if interactive || args[0] != "status" { // status prints it itself
		s.report()
	}

	if !interactive {
		if err := s.dispatch(ui, *payload, args); err != nil {
			ui.fail(err)
			return 1
		}
		return 0
	}
	for {
		fmt.Println()
		for _, k := range []string{"m1", "m2", "m3", "m4", "m5", "m6", "m0"} {
			fmt.Println("  " + tr(k))
		}
		var args []string
		switch ui.ask(tr("choose")) {
		case "1":
			args = []string{"install"}
		case "2":
			args = []string{"diagnostics"}
		case "3":
			dir := ui.ask(tr("pluginDir"))
			if dir == "" {
				continue
			}
			args = []string{"plugin", dir}
		case "4":
			args = []string{"uninstall"}
		case "5":
			args = []string{"keys"}
		case "6":
			args = []string{"status"}
		case "0", "q", "":
			return 0
		default:
			continue
		}
		if err := s.dispatch(ui, *payload, args); err != nil {
			ui.fail(err)
		}
	}
}

// dispatch runs one command.
func (s *session) dispatch(ui *ui, payload string, args []string) error {
	switch args[0] {
	case "install", "update":
		return s.install(payload)
	case "diagnostics":
		return s.diagnostics()
	case "plugin":
		if len(args) < 2 {
			return errors.New("plugin needs a folder")
		}
		return s.plugin(args[1])
	case "uninstall":
		if !ui.confirm(tr("confirmUninstall")) {
			fmt.Println(tr("aborted"))
			return nil
		}
		return s.device(s.out, "uninstall")
	case "stop", "start", "resetaccess":
		return s.device(s.out, args[0])
	case "keys":
		return s.keys()
	case "status":
		s.report()
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

// ---- SSH

type session struct {
	target  string
	opts    []string
	control string    // multiplexing socket dir (Linux/macOS)
	out     io.Writer // where progress goes (os.Stdout for the CLI, a buffer for the GUI)
	dev     *deviceInfo
}

func newSession(user, host, key string) (*session, error) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return nil, errors.New(tr("noSSH"))
	}
	// Neither may start with "-": ssh would read it as an option.
	if !regexp.MustCompile(`^[A-Za-z0-9:][A-Za-z0-9._:-]*$`).MatchString(host) || !regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`).MatchString(user) {
		return nil, errors.New(tr("badHost"))
	}
	s := &session{target: user + "@" + host, out: os.Stdout, opts: []string{
		"-o", "StrictHostKeyChecking=accept-new", "-o", "ConnectTimeout=8", "-o", "LogLevel=ERROR",
		"-o", "HostKeyAlgorithms=+ssh-rsa", "-o", "PubkeyAcceptedKeyTypes=+ssh-rsa",
	}}
	if key != "" {
		protectKey(key)
		s.opts = append(s.opts, "-i", key)
	}
	// One connection for all steps, so a password is asked for only once.
	// Windows' OpenSSH does not support this; "keys" helps there.
	if runtime.GOOS != "windows" {
		if dir, err := os.MkdirTemp("", "rcplug-ssh-"); err == nil {
			s.control = dir
			s.opts = append(s.opts, "-o", "ControlMaster=auto", "-o", "ControlPath="+filepath.Join(dir, "cm"), "-o", "ControlPersist=120")
		}
	}
	return s, nil
}

// protectKey makes a private key file readable by its owner only, as ssh
// demands on macOS and Linux: a key saved by a browser (the web page makes
// one) or copied around usually is not, and ssh would refuse it.
func protectKey(key string) {
	if runtime.GOOS == "windows" {
		return
	}
	if fi, err := os.Stat(key); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(key, fi.Mode().Perm()&0o700)
	}
}

func (s *session) close() {
	if s.control != "" {
		_ = exec.Command("ssh", append(append([]string{}, s.opts...), "-O", "exit", s.target)...).Run()
		os.RemoveAll(s.control)
	}
}

// ssh runs a command on the device. Password prompts go to the terminal.
func (s *session) ssh(stdin io.Reader, stdout io.Writer, remote ...string) error {
	return s.sshTo(stdin, stdout, os.Stderr, remote...)
}

func (s *session) sshTo(stdin io.Reader, stdout, stderr io.Writer, remote ...string) error {
	cmd := exec.Command("ssh", append(append(append([]string{}, s.opts...), s.target), remote...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return cmd.Run()
}

// device runs a command of rcplug-remote.sh on the device; the script is
// sent on stdin.
func (s *session) device(stdout io.Writer, args ...string) error {
	words := []string{"sh", "-s", "--"}
	for _, a := range args {
		words = append(words, shellQuote(a))
	}
	return s.ssh(bytes.NewReader(installer.RemoteScript), stdout, words...)
}

func (s *session) upload(r io.Reader, remotePath string) error {
	return s.ssh(r, s.out, "cat > "+shellQuote(remotePath))
}

// shellQuote makes s one word for the device's shell: ssh joins the words of
// the remote command with spaces and hands the line to the shell there, so
// a key comment like "Sam's laptop" or "$(...)" would otherwise be parsed.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func (s *session) probe() error {
	var out bytes.Buffer
	cmd := exec.Command("ssh", append(append(append([]string{}, s.opts...), s.target), "echo ok")...)
	var errb bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, &out, io.MultiWriter(os.Stderr, &errb)
	if err := cmd.Run(); err != nil || strings.TrimSpace(out.String()) != "ok" {
		if strings.Contains(errb.String(), "REMOTE HOST IDENTIFICATION HAS CHANGED") || strings.Contains(errb.String(), "Host key verification failed") {
			return errors.New(tr("hostKeyChanged"))
		}
		return errors.New(tr("sshFail"))
	}
	return nil
}

// deviceInfo is what the device itself is: its model (from the hostname the
// stock image sets) and firmware version.
type deviceInfo struct {
	Hostname  string `json:"hostname"`
	Firmware  string `json:"firmware"`
	Model     string `json:"model"` // fwimage model ID, "" if unknown
	ModelName string `json:"modelName"`
}

// deviceInfo asks the device once per session.
func (s *session) deviceInfo() deviceInfo {
	if s.dev != nil {
		return *s.dev
	}
	var out bytes.Buffer
	var d deviceInfo
	if s.device(&out, "device") != nil {
		return d
	}
	defer func() { s.dev = &d }()
	for _, line := range strings.Split(out.String(), "\n") {
		k, v, _ := strings.Cut(strings.TrimSpace(line), "=")
		switch k {
		case "hostname":
			d.Hostname = v
		case "firmware":
			d.Firmware = v
		}
	}
	if m, ok := fwimage.ModelByHostname(d.Hostname); ok {
		d.Model, d.ModelName = m.ID, m.Name
	}
	return d
}

func (d deviceInfo) String() string {
	name := d.ModelName
	if name == "" {
		name = tr("unknownModel") + " (" + d.Hostname + ")"
	}
	fw := d.Firmware
	if fw == "" {
		fw = "?"
	}
	return name + ", " + tr("firmwareWord") + " " + fw
}

// report prints what the device is, the host state and problems the device
// logged.
func (s *session) report() {
	if d := s.deviceInfo(); d.Hostname != "" || d.Firmware != "" {
		fmt.Fprintln(s.out, tr("deviceLine")+" "+d.String())
	}
	var out bytes.Buffer
	// The device's own "rcplug is not installed" (stderr) is said below in
	// the user's language.
	_ = s.sshTo(bytes.NewReader(installer.RemoteScript), &out, io.Discard, "sh", "-s", "--", "check")
	text := strings.TrimSpace(out.String())
	if text == "" {
		fmt.Fprintln(s.out, tr("notInstalled"))
		return
	}
	first, rest, _ := strings.Cut(text, "\n")
	fmt.Fprintln(s.out, tr("state")+" "+first)
	if rest != "" && rest != "logs: no problems found" {
		fmt.Fprintln(s.out, tr("problems"))
		lines := strings.Split(rest, "\n")
		if len(lines) > 12 {
			lines = lines[:12]
		}
		for _, l := range lines {
			fmt.Fprintln(s.out, "     "+l)
		}
		fmt.Fprintln(s.out, tr("problemsHint"))
	}
}

// ---- commands

func (s *session) install(payload string) error {
	if payload == "" {
		payload = newestPayload()
	}
	if payload == "" { // none at hand: rcplug's latest release
		p, err := latestPayload(s.out)
		if err != nil {
			return fmt.Errorf("%s %v", tr("noPayload"), err)
		}
		payload = p
	}
	if err := checkPayload(payload); err != nil {
		return err
	}
	f, err := os.Open(payload)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintln(s.out, tr("sending")+" "+filepath.Base(payload)+" …")
	if err := s.upload(f, "/tmp/rcplug-payload.tgz"); err != nil {
		return err
	}
	fmt.Fprintln(s.out, tr("installing"))
	return s.device(s.out, "install", "/tmp/rcplug-payload.tgz")
}

// latestPayload fetches the device package of rcplug's latest release from
// GitHub into the user's cache folder, checked as rcplugd checks its own
// updates: a manifest signed with rcplug's release key, and the package's
// size and SHA-256 as it names them.
func latestPayload(out io.Writer) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c := rcdl.Client()
	p, err := rcdl.Latest(ctx, c)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(out, tr("fetchingRcplug")+"\n", p.Version)
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	file, err := rcdl.Download(ctx, c, p, filepath.Join(dir, "rcplug-setup"))
	if err != nil {
		return "", err
	}
	fmt.Fprintln(out, tr("rcplugVerified"), file)
	return file, nil
}

// newestPayload finds the newest release tarball next to the program or in
// the current folder (the examples tarball is skipped).
func newestPayload() string {
	var found []string
	dirs := []string{"."}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	re := regexp.MustCompile(`^rcplug-[0-9][^/]*-aarch64\.tar\.gz$`)
	for _, d := range dirs {
		ents, _ := os.ReadDir(d)
		for _, e := range ents {
			if re.MatchString(e.Name()) {
				found = append(found, filepath.Join(d, e.Name()))
			}
		}
	}
	if len(found) == 0 {
		return ""
	}
	sort.Slice(found, func(i, j int) bool { return versionLess(filepath.Base(found[i]), filepath.Base(found[j])) })
	return found[len(found)-1]
}

// versionLess compares release file names by their numeric version parts.
func versionLess(a, b string) bool {
	num := regexp.MustCompile(`\d+`)
	x, y := num.FindAllString(a, -1), num.FindAllString(b, -1)
	for i := 0; i < len(x) && i < len(y); i++ {
		if len(x[i]) != len(y[i]) {
			return len(x[i]) < len(y[i])
		}
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return len(x) < len(y)
}

func checkPayload(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("%s contains no rcplugd", path)
		}
		if err != nil {
			return fmt.Errorf("%s: %v", path, err)
		}
		if strings.TrimPrefix(h.Name, "./") == "rcplugd" {
			return nil
		}
	}
}

func (s *session) diagnostics() error {
	name := fmt.Sprintf("rcplug-diagnostics-%s-%s.zip", strings.Split(s.target, "@")[1], time.Now().Format("20060102-150405"))
	var buf bytes.Buffer
	if err := s.device(&buf, "diagnostics"); err != nil {
		return err
	}
	if buf.Len() == 0 {
		return errors.New(tr("noDiag"))
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("PK")) {
		name = strings.TrimSuffix(name, ".zip") + ".tar.gz" // raw logs
	}
	name = strings.ReplaceAll(name, ":", "-")
	if err := os.WriteFile(name, buf.Bytes(), 0o644); err != nil {
		return err
	}
	abs, _ := filepath.Abs(name)
	fmt.Fprintln(s.out, tr("saved")+" "+abs)
	fmt.Fprintln(s.out, tr("diagNote"))
	return nil
}

var pluginName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func (s *session) plugin(dir string) error {
	dir = strings.Trim(strings.TrimSpace(dir), `"'`)
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	name := filepath.Base(abs)
	if _, err := os.Stat(filepath.Join(abs, "plugin.json")); err != nil || !pluginName.MatchString(name) {
		return errors.New(tr("badPlugin"))
	}
	var buf bytes.Buffer
	if err := tarDir(&buf, abs); err != nil {
		return err
	}
	remote := "/tmp/rcplug-plugin-" + name + ".tgz"
	if err := s.upload(&buf, remote); err != nil {
		return err
	}
	return s.device(s.out, "plugin", name, remote)
}

// tarDir packs a directory as tar.gz with Unix permissions: the plugin's own
// program (named like the folder) and *.sh are executable.
func tarDir(w io.Writer, dir string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	name := filepath.Base(dir)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr := &tar.Header{Name: rel, ModTime: info.ModTime(), Mode: 0o644, Typeflag: tar.TypeReg, Size: info.Size()}
		if d.IsDir() {
			hdr.Typeflag, hdr.Mode, hdr.Size, hdr.Name = tar.TypeDir, 0o755, 0, rel+"/"
			return tw.WriteHeader(hdr)
		}
		if !info.Mode().IsRegular() {
			return nil // links and devices are not part of a plugin
		}
		if rel == name || strings.HasSuffix(rel, ".sh") || info.Mode()&0o111 != 0 {
			hdr.Mode = 0o755
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// keys installs this computer's SSH public key on the device (creating one
// with ssh-keygen if needed), so later logins need no password.
func (s *session) keys() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	var pub string
	for _, k := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		if _, err := os.Stat(filepath.Join(home, ".ssh", k+".pub")); err == nil {
			pub = filepath.Join(home, ".ssh", k+".pub")
			break
		}
	}
	if pub == "" {
		fmt.Fprintln(s.out, tr("keygen"))
		_ = os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
		priv := filepath.Join(home, ".ssh", "id_ed25519")
		cmd := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", priv)
		cmd.Stdout, cmd.Stderr = s.out, os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
		pub = priv + ".pub"
	}
	raw, err := os.ReadFile(pub)
	if err != nil {
		return err
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(raw)), "\n")
	fmt.Fprintln(s.out, tr("keySend"))
	// addkey leaves a key that is already there alone.
	if err := s.device(s.out, append([]string{"addkey"}, strings.Fields(line)...)...); err != nil {
		return err
	}
	fmt.Fprintln(s.out, tr("keyOK"))
	return nil
}

// ---- terminal

type ui struct {
	in  *bufio.Reader
	yes bool
}

func (u *ui) ask(prompt string) string {
	fmt.Print(prompt)
	line, _ := u.in.ReadString('\n')
	return strings.TrimSpace(line)
}

func (u *ui) confirm(prompt string) bool {
	if u.yes {
		return true
	}
	a := strings.ToLower(u.ask(prompt))
	return a == "yes" || a == "ja" || a == "y" || a == "j"
}

func (u *ui) fail(err error) {
	fmt.Fprintln(os.Stderr, "\n"+tr("error")+" "+err.Error())
}

func (u *ui) waitBeforeExit() {
	if runtime.GOOS == "windows" {
		fmt.Print("\n" + tr("pressEnter"))
		_, _ = u.in.ReadString('\n')
	}
}
