package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"memento-mcp/internal/mcp"
)

type doctorOptions struct {
	clients []string
}

func parseDoctorFlags(args []string) (doctorOptions, error) {
	var opts doctorOptions
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--client" || a == "--clients":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("%s requires a value", a)
			}
			i++
			opts.clients = appendClientValues(opts.clients, args[i])
		case strings.HasPrefix(a, "--client="):
			opts.clients = appendClientValues(opts.clients, strings.TrimPrefix(a, "--client="))
		case strings.HasPrefix(a, "--clients="):
			opts.clients = appendClientValues(opts.clients, strings.TrimPrefix(a, "--clients="))
		default:
			return opts, fmt.Errorf("unknown option %q", a)
		}
	}
	return opts, nil
}

func runDoctor(args []string, stdout io.Writer) error {
	opts, err := parseDoctorFlags(args)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	return doctorClients(knownClients(exe), opts.clients, exe, stdout)
}

func doctorClients(clients []mcpClient, selectedSlugs []string, exe string, stdout io.Writer) error {
	fmt.Fprintln(stdout, "memento-mcp doctor")
	failures := 0
	if err := validateExecutable(exe); err != nil {
		fmt.Fprintf(stdout, "[FAIL] binary: %v\n", err)
		failures++
	} else {
		fmt.Fprintf(stdout, "[PASS] binary: %s (%s, %s/%s)\n", shortenPath(exe), mcp.ServerVersion(), runtime.GOOS, runtime.GOARCH)
	}
	failures += doctorSemantic(context.Background(), stdout)

	selected := clients
	explicit := len(selectedSlugs) > 0
	if explicit {
		var err error
		selected, err = filterClients(clients, selectedSlugs)
		if err != nil {
			return err
		}
	}

	checked := 0
	for _, client := range selected {
		state, err := inspectClient(client, exe)
		if err != nil {
			fmt.Fprintf(stdout, "[FAIL] %s: %v\n", client.Name, err)
			failures++
			checked++
			continue
		}
		if !explicit && (state.Status == "not configured" || state.Status == "unavailable") {
			continue
		}
		checked++
		switch state.Status {
		case "configured":
			fmt.Fprintf(stdout, "[PASS] %s: registered as %s\n", client.Name, state.Name)
		case "unavailable":
			fmt.Fprintf(stdout, "[FAIL] %s: %s command is not installed\n", client.Name, client.CLI)
			failures++
		case "not configured":
			fmt.Fprintf(stdout, "[FAIL] %s: Memento is not registered\n", client.Name)
			failures++
		case "stale":
			fmt.Fprintf(stdout, "[FAIL] %s: registration points to another executable\n", client.Name)
			failures++
		default:
			fmt.Fprintf(stdout, "[FAIL] %s: %s\n", client.Name, state.Status)
			failures++
		}
	}
	if checked == 0 {
		fmt.Fprintln(stdout, "[WARN] clients: no Memento client registrations found")
	}
	if failures > 0 {
		return fmt.Errorf("%d diagnostic check(s) failed", failures)
	}
	fmt.Fprintln(stdout, "Healthy.")
	return nil
}

func validateExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("path is not a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return errors.New("file is not executable")
	}
	return nil
}
