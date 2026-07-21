package cli

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ychiu1211/dsmctl/internal/config"
	"github.com/ychiu1211/dsmctl/internal/provision"
)

func newInstallCommand(opts *options) *cobra.Command {
	var targetURL, patFile, adminUser, deviceName, autoUpdate, profileName, raidType, filesystem string
	var doInstall, assumeYes, analytics, createVolume, allowUnsupportedDisks bool
	var timeoutMinutes, passwordLength int
	command := &cobra.Command{
		Use:   "install",
		Short: "Detect a never-installed/crashed NAS and install DSM online",
		Long: "Query a Synology device's Web Assistant for its install state and, with\n" +
			"--install, trigger an ONLINE DSM installation (the device downloads DSM\n" +
			"from Synology, installs it, and reboots). Installing is DESTRUCTIVE: it\n" +
			"writes the OS to the device's disks, so it requires --install and a\n" +
			"confirmation (--yes to skip the prompt). Without --install the command\n" +
			"only reports state and changes nothing.\n\n" +
			"Prefer to run bring-up in stages: install here, then 'dsmctl provision'\n" +
			"for the first administrator, then storage separately once the disk layout\n" +
			"is decided (storage is a deliberate choice, not a default). For a\n" +
			"simple/unattended box you can chain them: --admin-user also creates the\n" +
			"first administrator and finishes the wizard after install, and\n" +
			"--create-volume then builds one all-disk volume (default btrfs RAID5) —\n" +
			"but that single command blocks through install, reboot, setup, and volume\n" +
			"creation, so use it only when no storage discussion is needed.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(targetURL) == "" {
				return errors.New("--url is required, e.g. http://10.17.36.255:5000")
			}
			if createVolume && strings.TrimSpace(adminUser) == "" {
				return errors.New("--create-volume needs --admin-user: the volume is created as the provisioned administrator")
			}
			target := provision.Target{BaseURL: targetURL, HTTPClient: installHTTPClient()}
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			state, err := provision.GetState(ctx, target)
			if err != nil {
				return fmt.Errorf("read install state: %w", err)
			}
			writeInstallState(out, state)
			if state.Installed() {
				fmt.Fprintln(out, "\nDSM appears to be installed already; nothing to do.")
				return nil
			}
			need := state.NeedsInstall()
			if need == "" {
				return fmt.Errorf("device reported an unrecognized state %q; not installing", state.Status)
			}
			plan := state.OnlineInstallPlan()
			if plan.Status == "" {
				return fmt.Errorf("state %q does not support a plain install", state.Status)
			}
			// The offline path (host downloads the .pat and uploads it) is used
			// whenever an explicit --pat is given or the device itself cannot reach
			// Synology. Its status parameter is the same as the online path.
			offline := strings.TrimSpace(patFile) != "" || !plan.Available
			var patURL, patName string
			if offline && strings.TrimSpace(patFile) == "" {
				patURL, patName, err = provision.DefaultPatURL(state)
				if err != nil {
					return fmt.Errorf("cannot determine a DSM image to install offline: %w", err)
				}
			}
			if !doInstall {
				if !offline {
					fmt.Fprintf(out, "\nOnline %s is available (%s). Re-run with --install to perform it (DESTRUCTIVE: wipes the disks).\n", plan.Kind, plan.Version)
				} else if strings.TrimSpace(patFile) != "" {
					fmt.Fprintf(out, "\nThe device has no online install; --install would upload %s and install it (DESTRUCTIVE).\n", patFile)
				} else {
					fmt.Fprintf(out, "\nThe device has no internet route. --install would download the matching image and upload it (DESTRUCTIVE):\n  %s\n", patURL)
				}
				return nil
			}
			migrate := plan.Kind == "migrate"
			if migrate {
				fmt.Fprintln(out, "\nThis is a MIGRATION: it installs DSM while preserving the data on the existing disks. It has not been live-verified by dsmctl.")
			}
			if !assumeYes {
				source := "ONLINE"
				if offline {
					source = "OFFLINE (upload .pat)"
				}
				consequence := "ERASE its disks"
				if migrate {
					consequence = "preserve the existing data (migration)"
				}
				fmt.Fprintf(cmd.ErrOrStderr(),
					"This will %s-%s DSM on %s and %s.\nType the device serial (%s) to confirm: ",
					source, strings.ToUpper(plan.Kind), state.IP, consequence, state.Serial)
				if !confirmSerial(cmd.InOrStdin(), state.Serial) {
					return errors.New("confirmation did not match the device serial; nothing was installed")
				}
			}
			setupURL := installSetupURL(state)
			if offline {
				if err := runOfflineInstall(ctx, target, state.Status, patFile, patURL, patName, out); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(out, "\nStarting online %s of %s ...\n", plan.Kind, plan.Version)
				if err := provision.InstallOnline(ctx, target, plan.Status); err != nil {
					return err
				}
			}
			if err := waitForInstall(ctx, target, setupURL, out, time.Duration(timeoutMinutes)*time.Minute); err != nil {
				return err
			}
			fmt.Fprintf(out, "\nDSM installed. The NAS is now in first-run setup at %s\n", setupURL)
			if strings.TrimSpace(adminUser) == "" {
				fmt.Fprintf(out, "Create the first administrator with:\n    dsmctl provision <name> --url %s --admin-user <user>\n", setupURL)
				return nil
			}
			// Combined install → first-admin. Give the setup subsystem a moment to
			// settle after first boot (a just-up DSM can briefly reject the setup
			// session) before creating the administrator.
			name := profileName
			if strings.TrimSpace(name) == "" {
				name = deriveInstallProfileName(state)
			}
			fmt.Fprintf(out, "\nWaiting for the setup wizard to be ready, then creating administrator %q...\n", adminUser)
			if err := sleepCtx(ctx, 20*time.Second); err != nil {
				return err
			}
			store := config.NewStore(opts.configPath)
			cfg, err := store.Load()
			if err != nil {
				return err
			}
			service, err := loadService(opts)
			if err != nil {
				return err
			}
			defer closeService(service)
			profile := config.Profile{URL: setupURL, Username: adminUser, InsecureSkipTLSVerify: true}
			req := firstAdminRequest{name: name, adminUser: adminUser, deviceName: deviceName, autoUpdate: autoUpdate, analytics: analytics, length: passwordLength, profile: profile}
			if err := runFirstAdmin(cmd, store, cfg, service, req); err != nil {
				return fmt.Errorf("DSM is installed, but creating the administrator failed (retry with 'dsmctl provision %s --url %s --admin-user %s --insecure-skip-tls-verify'): %w", name, setupURL, adminUser, err)
			}
			if !createVolume {
				fmt.Fprintf(out, "\nNext: create storage so the NAS is usable — run 'dsmctl storage' (or re-run install with --create-volume).\n")
				return nil
			}
			// Reload the service so it sees the just-provisioned profile and its
			// stored credential, then create the first volume as that administrator.
			volumeService, err := loadService(opts)
			if err != nil {
				return fmt.Errorf("DSM installed and administrator created, but reloading to create storage failed (run 'dsmctl storage' against %q): %w", name, err)
			}
			defer closeService(volumeService)
			fmt.Fprintln(out)
			if err := createAllDiskVolume(cmd, volumeService, name, raidType, filesystem, allowUnsupportedDisks); err != nil {
				return fmt.Errorf("DSM installed and administrator %q created, but creating the storage volume failed (finish it with 'dsmctl storage' or the nas-storage-setup skill): %w", adminUser, err)
			}
			fmt.Fprintf(out, "\nNAS %q is fully set up and ready to use.\n", name)
			return nil
		},
	}
	command.Flags().StringVar(&targetURL, "url", "", "Web Assistant URL of the device, e.g. http://10.17.36.255:5000")
	command.Flags().StringVar(&patFile, "pat", "", "install from a local .pat file (offline); when the device has no internet, the matching image is auto-downloaded")
	command.Flags().BoolVar(&doInstall, "install", false, "actually perform the install (DESTRUCTIVE); without it, only report state")
	command.Flags().BoolVar(&assumeYes, "yes", false, "skip the interactive serial confirmation (for automation)")
	command.Flags().IntVar(&timeoutMinutes, "timeout-minutes", 30, "how long to wait for the install and reboot to complete")
	command.Flags().StringVar(&adminUser, "admin-user", "", "after install, create this first administrator (combined install→provision; password stored in the OS credential store)")
	command.Flags().StringVar(&profileName, "profile-name", "", "NAS profile name to record when --admin-user is set (default derived from the device)")
	command.Flags().StringVar(&deviceName, "device-name", "", "DSM server name (hostname) to set when creating the administrator")
	command.Flags().StringVar(&autoUpdate, "auto-update", "security", "DSM update policy for the created administrator: security, all, or notify")
	command.Flags().BoolVar(&analytics, "analytics", false, "opt in to Synology device analytics when creating the administrator")
	command.Flags().IntVar(&passwordLength, "length", 24, "generated administrator password length")
	command.Flags().BoolVar(&createVolume, "create-volume", false, "after provisioning, create one storage volume across ALL disks (requires --admin-user; DESTRUCTIVE)")
	command.Flags().StringVar(&raidType, "raid", "raid5", "RAID type for --create-volume: raid5, raid6, raid10, raid1, shr, shr2, jbod, or basic")
	command.Flags().StringVar(&filesystem, "filesystem", "btrfs", "filesystem for --create-volume: btrfs or ext4")
	command.Flags().BoolVar(&allowUnsupportedDisks, "allow-unsupported-disks", false, "let --create-volume use drives DSM does not list as compatible (lab/unvalidated drives)")
	return command
}

// deriveInstallProfileName produces a config-safe NAS profile name for the
// combined install→provision flow when the caller does not supply one.
func deriveInstallProfileName(state provision.DeviceState) string {
	base := strings.TrimSpace(state.Hostname)
	if base == "" || strings.EqualFold(base, "DiskStation") {
		base = strings.TrimSpace(state.IP)
	}
	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, base)
	sanitized = strings.Trim(sanitized, "-_.")
	if sanitized == "" {
		return "provisioned-nas"
	}
	return "nas-" + sanitized
}

func writeInstallState(out io.Writer, state provision.DeviceState) {
	fmt.Fprintf(out, "Model:   %s (serial %s)\n", state.Model, state.Serial)
	fmt.Fprintf(out, "Address: %s  MAC %s\n", state.IP, state.MAC)
	fmt.Fprintf(out, "Disks:   %d present, size sufficient=%t\n", state.DiskCount, state.DiskSizeEnough)
	fmt.Fprintf(out, "State:   %s\n", installStateLabel(state.Status))
	if plan := state.OnlineInstallPlan(); plan.Status != "" {
		availability := "not reachable online"
		if plan.Available {
			availability = "available online: " + plan.Version
		}
		fmt.Fprintf(out, "Online %s: %s\n", plan.Kind, availability)
	}
}

func installStateLabel(status string) string {
	switch status {
	case "not_install":
		return "not_install (no DSM installed)"
	case "sys_crash":
		return "sys_crash (DSM installed but broken; needs reinstall)"
	case "sys_migrat":
		return "sys_migrat (disks from another NAS; migration)"
	case "":
		return "installed / running"
	default:
		return status
	}
}

func installSetupURL(state provision.DeviceState) string {
	port := strings.TrimSpace(state.HTTPSAdminPort)
	if port == "" {
		port = "5001"
	}
	return "https://" + state.IP + ":" + port
}

// waitForInstall polls install progress, then waits for the device to reboot and
// come back online after DSM is installed. Completion is detected by the setup
// URL's SYNO.API.Info returning JSON: once DSM is running it takes over the port
// and the Web Assistant's get_state.cgi stops serving, so the assistant endpoint
// alone cannot confirm success (live-verified on DSM 7.3.1).
func waitForInstall(ctx context.Context, target provision.Target, setupURL string, out io.Writer, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	lastLine := ""
	sawProgress := false
	for time.Now().Before(deadline) {
		if err := sleepCtx(ctx, 5*time.Second); err != nil {
			return err
		}
		progress, err := provision.GetInstallProgress(ctx, target)
		if err != nil {
			// The device likely went down to reboot; move to the reboot-wait phase.
			if sawProgress {
				break
			}
			continue
		}
		if progress.Raw != "" && (progress.Stage != "" || progress.Percent > 0) {
			sawProgress = true
			line := fmt.Sprintf("  %s... %d%%", progress.Stage, progress.Percent)
			if line != lastLine {
				fmt.Fprintln(out, line)
				lastLine = line
			}
		}
		if progress.Raw == "" && sawProgress {
			fmt.Fprintln(out, "  install step complete; the NAS is rebooting...")
			break
		}
	}
	// Reboot-wait: the device is unreachable while it reboots, then comes back in
	// first-run setup with DSM serving its API. DSM answering SYNO.API.Info is the
	// authoritative "installed and up" signal.
	fmt.Fprintln(out, "  waiting for the NAS to reboot and DSM to come up...")
	client := installHTTPClient()
	for time.Now().Before(deadline) {
		if err := sleepCtx(ctx, 10*time.Second); err != nil {
			return err
		}
		if dsmIsUp(ctx, client, setupURL) {
			return nil
		}
	}
	return fmt.Errorf("timed out after %s waiting for the install to complete; check the NAS directly", timeout)
}

// dsmIsUp reports whether DSM is installed and serving its WebAPI at the setup
// URL (as opposed to the Web Assistant, which redirects instead of returning
// JSON while DSM is not installed).
func dsmIsUp(ctx context.Context, client *http.Client, setupURL string) bool {
	endpoint := strings.TrimRight(setupURL, "/") + "/webapi/query.cgi?api=SYNO.API.Info&version=1&method=query&query=SYNO.API.Auth"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return false
	}
	return strings.Contains(string(body), `"SYNO.API.Auth"`) && strings.Contains(string(body), `"success":true`)
}

// runOfflineInstall installs from a .pat image: a local file if given, otherwise
// the matching image downloaded from Synology to a temporary file. The image is
// then streamed to the device's install.cgi. The host needs internet even when
// the device does not.
func runOfflineInstall(ctx context.Context, target provision.Target, status, patFile, patURL, patName string, out io.Writer) error {
	path := patFile
	if strings.TrimSpace(path) == "" {
		fmt.Fprintf(out, "\nThe device has no internet route; downloading the matching image on this host:\n  %s\n", patURL)
		downloaded, cleanup, err := downloadPat(ctx, patURL, patName, out)
		if err != nil {
			return err
		}
		defer cleanup()
		path = downloaded
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open .pat image: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat .pat image: %w", err)
	}
	name := patName
	if name == "" {
		name = filepath.Base(path)
	}
	fmt.Fprintf(out, "Uploading %s (%d MB) and starting install (this transfers the whole image)...\n", name, info.Size()/(1024*1024))
	// The whole image is streamed in one request, which far exceeds the short
	// client's overall timeout, so the upload uses a client bounded only by dial/
	// header timeouts and the command context.
	uploadTarget := provision.Target{BaseURL: target.BaseURL, HTTPClient: installUploadClient()}
	return provision.InstallLocal(ctx, uploadTarget, status, name, file, info.Size())
}

func downloadPat(ctx context.Context, patURL, patName string, out io.Writer) (string, func(), error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, patURL, nil)
	if err != nil {
		return "", func() {}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", func() {}, fmt.Errorf("download DSM image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", func() {}, fmt.Errorf("download DSM image: Synology returned HTTP %d for %s", resp.StatusCode, patURL)
	}
	name := patName
	if name == "" {
		name = "dsm.pat"
	}
	tmp, err := os.CreateTemp("", "dsmctl-*-"+name)
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.Remove(tmp.Name()) }
	written, err := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("write DSM image: %w", err)
	}
	if closeErr != nil {
		cleanup()
		return "", func() {}, closeErr
	}
	fmt.Fprintf(out, "Downloaded %d MB.\n", written/(1024*1024))
	return tmp.Name(), cleanup, nil
}

func installHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: insecureInstallTransport()}
}

// installUploadClient streams a whole DSM image in one request, so it has no
// overall timeout; dial and response-header timeouts still bound a dead server.
func installUploadClient() *http.Client {
	transport := insecureInstallTransport()
	transport.ResponseHeaderTimeout = 10 * time.Minute
	return &http.Client{Transport: transport}
}

func insecureInstallTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The device is factory-fresh with a self-signed certificate and the install
	// calls carry no password, so certificate verification is not meaningful here.
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // fresh-device install channel carries no secret
	return transport
}

func confirmSerial(input io.Reader, serial string) bool {
	line, _ := bufio.NewReader(input).ReadString('\n')
	return strings.TrimSpace(serial) != "" && strings.EqualFold(strings.TrimSpace(line), strings.TrimSpace(serial))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
