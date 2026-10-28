// Package setup guides profile creation without reading credential values or connecting.
package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/ficaa1/micko/internal/argo"
	"github.com/ficaa1/micko/internal/config"
	"gopkg.in/yaml.v3"
)

var errCanceled = errors.New("setup canceled")
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

type guide struct {
	input  *bufio.Scanner
	output io.Writer
}

func (g *guide) ask(label, fallback string) (string, error) {
	if fallback != "" {
		label += " [" + fallback + "]"
	}
	if _, err := fmt.Fprint(g.output, label+": "); err != nil {
		return "", err
	}
	if !g.input.Scan() {
		if err := g.input.Err(); err != nil {
			return "", errors.New("could not read setup input")
		}
		return "", errCanceled
	}
	s := strings.TrimSpace(g.input.Text())
	if s == ":cancel" || s == "\x03" {
		return "", errCanceled
	}
	if strings.ContainsFunc(s, unicode.IsControl) {
		return "", errors.New("setup fields cannot contain control characters")
	}
	if s == "" {
		s = fallback
	}
	return s, nil
}

// Run saves a validated profile only after explicit confirmation; EOF cancels.
func Run(input io.Reader, output io.Writer, path string) (err error) {
	defer func() {
		if errors.Is(err, errCanceled) {
			_, err = fmt.Fprintln(output, "\nSetup canceled; no profile saved.")
		}
	}()
	g := guide{input: bufio.NewScanner(input), output: output}
	fmt.Fprintln(output, "Create a micko profile. No cluster connection is made. Enter :cancel or send EOF to cancel.")
	fmt.Fprintln(output, "Enter only token environment variable names or absolute token file paths, never token values.")
	name, err := g.ask("Profile name", "")
	if err != nil {
		return err
	}
	if !namePattern.MatchString(name) {
		return errors.New("invalid profile name; use letters, digits, dots, underscores or hyphens, starting with a letter or digit")
	}
	mode, err := g.ask("Connection (direct/forward)", "direct")
	if err != nil {
		return err
	}
	if mode != "direct" && mode != "forward" {
		return errors.New("connection must be direct or forward")
	}
	p := config.Profile{}
	endpointDefault := ""
	if mode == "forward" {
		endpointDefault = "https://localhost:2746"
	}
	p.Server, err = g.ask("Argo endpoint (scheme and base path; forward replaces host/port)", endpointDefault)
	if err != nil {
		return err
	}
	p.Namespace, err = g.ask("Workflow namespace", "argo")
	if err != nil {
		return err
	}
	if mode == "forward" {
		p.KubeContext, err = g.ask("Kubernetes context", "")
		if err != nil {
			return err
		}
		p.ServiceNamespace, err = g.ask("Argo service namespace", p.Namespace)
		if err != nil {
			return err
		}
		p.Service, err = g.ask("Argo service name", "argo-server")
		if err != nil {
			return err
		}
		port, e := g.ask("Argo service remote port", "2746")
		if e != nil {
			return e
		}
		p.RemotePort, err = strconv.Atoi(port)
		if err != nil {
			return errors.New("remote port must be a number from 1 to 65535")
		}
	}
	fmt.Fprintln(output, "Use server for Argo --auth-mode=server; token for client or SSO bearer authentication.")
	p.AuthMode, err = g.ask("Authentication (server/token)", "token")
	if err != nil {
		return err
	}
	switch p.AuthMode {
	case "server":
	case "token":
		source, e := g.ask("Token source (env/file)", "env")
		if e != nil {
			return e
		}
		switch source {
		case "env":
			p.TokenEnv, err = g.ask("Token environment variable NAME", "")
			if err != nil {
				return err
			}
			if !envName.MatchString(p.TokenEnv) {
				return errors.New("token source must be an environment variable name, never a token value")
			}
		case "file":
			p.TokenFile, err = g.ask("Absolute token file path", "")
			if err != nil {
				return err
			}
			if p.TokenFile == "" {
				return errors.New("token file path is required")
			}
		default:
			return errors.New("token source must be env or file")
		}
	default:
		return errors.New("authentication must be server or token")
	}
	p.CAFile, err = g.ask("CA bundle path (optional; empty uses system trust)", "")
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(config.File{Profiles: map[string]config.Profile{name: p}})
	if err != nil {
		return err
	}
	if _, err := config.Load(data, config.Options{Profile: name}); err != nil {
		return errors.New("profile validation failed; check endpoint security, namespace, forwarding target and readable absolute token/CA paths; no profile saved")
	}
	// Validate CA content through the production transport without sending a request.
	if err := validateCA(p); err != nil {
		return err
	}
	confirm, err := g.ask("Save profile "+name+"? (yes/no)", "no")
	if err != nil {
		return err
	}
	if confirm != "yes" {
		return errCanceled
	}
	if err := config.AddProfile(path, name, p); err != nil {
		return errors.New("could not save profile: check the config file, existing profile names, permissions and .lock file; no profile saved")
	}
	fmt.Fprintln(output, "Profile saved. Check it with: micko --config "+strconv.Quote(path)+" --check --profile "+name)
	return nil
}

func validateCA(p config.Profile) error {
	client, err := argo.NewClient(argo.Options{Server: p.Server, CAFile: p.CAFile, TokenFn: func() (string, error) { return "", nil }})
	if err != nil {
		return errors.New("TLS configuration is invalid; caFile must be a readable PEM CA bundle; no profile saved")
	}
	client.Close()
	return nil
}
