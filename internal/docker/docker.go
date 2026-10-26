// Package docker drives the `docker` CLI for the greyman container lifecycle.
// It shells out rather than using the Docker SDK to stay dependency-free.
package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"greyman/internal/version"
)

const (
	Name      = "greyman"
	Volume    = "greyman-profile"
	Repo      = "https://github.com/RoundBunny/greyman.git" // build context for release images
	apiPort   = "7777"
	watchPort = "6080"
)

// DefaultImage is the image this CLI runs unless IMAGE says otherwise.
var DefaultImage = ImageFor(version.Version)

// ImageFor is the image a CLI of version v runs by default: a release runs its own tag,
// built by the install script (or by `up`); a dev build runs the `make image` output.
func ImageFor(v string) string {
	if version.IsRelease(v) {
		return "greyman:" + v
	}
	return "greyman:latest"
}

// BuildArgs builds release v's image from its tag on GitHub. Pure, so it can be unit tested.
func BuildArgs(v, image string) []string {
	return []string{"build", "-f", "docker/Dockerfile", "--build-arg", "VERSION=" + v, "-t", image, Repo + "#" + v}
}

// Build runs BuildArgs, streaming docker's progress to out.
func Build(ctx context.Context, v, image string, out io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", BuildArgs(v, image)...)
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}

type RunOpts struct {
	Image    string
	Platform string // e.g. "linux/amd64"; empty = image's native
	Token    string
	// Env is NAME=value for the container's settings. Only the names go on the command
	// line (`-e NAME`); the values travel in docker's own environment, so secrets such
	// as the proxy URL never show up in `ps`. The token gets the same treatment.
	Env       []string
	Watch     bool // publish a view-only noVNC page (headed mode only)
	APIPort   int  // host port to publish the API on; 0 = Docker picks
	WatchPort int  // same for the noVNC page
}

// RunArgs builds the `docker run` argument list. Pure, so it can be unit tested.
// Published ports are bound to loopback only.
func RunArgs(o RunOpts) []string {
	args := []string{
		"run", "-d", "--name", Name,
		"-p", publish(o.APIPort, apiPort),
		"--shm-size=1g", // Chrome crashes on the default 64MB /dev/shm
		"-v", Volume + ":/data",
	}
	if o.Watch {
		args = append(args, "-p", publish(o.WatchPort, watchPort))
	}
	if o.Platform != "" {
		args = append(args, "--platform", o.Platform)
	}
	args = append(args, "-e", "GREYMAN_TOKEN")
	for _, kv := range o.Env {
		name, _, _ := strings.Cut(kv, "=")
		args = append(args, "-e", name)
	}
	return append(args, o.Image)
}

// publish is the -p value for a loopback port; hostPort 0 lets Docker choose.
func publish(hostPort int, containerPort string) string {
	if hostPort == 0 {
		return "127.0.0.1::" + containerPort
	}
	return fmt.Sprintf("127.0.0.1:%d:%s", hostPort, containerPort)
}

// runEnv is the environment for the docker process: ours plus the per-run values.
func runEnv(o RunOpts) []string {
	env := append(os.Environ(), o.Env...)
	return append(env, "GREYMAN_TOKEN="+o.Token)
}

func docker(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("docker %s: %s", args[0], msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func ImageExists(ctx context.Context, image string) bool {
	_, err := docker(ctx, nil, "image", "inspect", image)
	return err == nil
}

// Running reports whether the named container exists and is running.
func Running(ctx context.Context, name string) bool {
	out, err := docker(ctx, nil, "inspect", "-f", "{{.State.Running}}", name)
	return err == nil && out == "true"
}

// IsDown reports whether docker answered and no container by that name is running. A
// failing docker (not installed, socket blocked by an agent sandbox) is "don't know",
// so it returns false.
func IsDown(ctx context.Context, name string) bool {
	out, err := docker(ctx, nil, "ps", "-q", "--filter", "name=^/"+name+"$")
	return err == nil && out == ""
}

// Ports are the loopback host ports Docker published. Watch is 0 unless requested.
type Ports struct{ API, Watch int }

// Run starts the container and returns the host ports it was published on.
func Run(ctx context.Context, o RunOpts) (Ports, error) {
	var p Ports
	if _, err := docker(ctx, runEnv(o), RunArgs(o)...); err != nil {
		return p, err
	}
	var err error
	if p.API, err = hostPort(ctx, apiPort); err != nil {
		return p, err
	}
	if o.Watch {
		p.Watch, err = hostPort(ctx, watchPort)
	}
	return p, err
}

func hostPort(ctx context.Context, containerPort string) (int, error) {
	out, err := docker(ctx, nil, "port", Name, containerPort+"/tcp")
	if err != nil {
		return 0, err
	}
	return parsePort(out)
}

// parsePort reads the first "127.0.0.1:49153" line of `docker port` output.
func parsePort(out string) (int, error) {
	line, _, _ := strings.Cut(out, "\n")
	_, p, err := net.SplitHostPort(strings.TrimSpace(line))
	if err != nil {
		return 0, fmt.Errorf("unexpected `docker port` output %q", out)
	}
	return strconv.Atoi(p)
}

// Remove stops (gracefully, so the profile flushes) and removes the container.
// A missing container is not an error. purge also deletes the profile volume.
func Remove(ctx context.Context, name string, purge bool) error {
	if Running(ctx, name) {
		if _, err := docker(ctx, nil, "stop", "-t", "10", name); err != nil {
			return err
		}
	}
	if _, err := docker(ctx, nil, "rm", "-f", name); err != nil && !strings.Contains(err.Error(), "No such container") {
		return err
	}
	if purge {
		if _, err := docker(ctx, nil, "volume", "rm", "-f", Volume); err != nil {
			return err
		}
	}
	return nil
}

// Logs returns the last lines of container output (stdout and stderr merged).
func Logs(ctx context.Context, name string, n int) string {
	out, err := exec.CommandContext(ctx, "docker", "logs", "--tail", strconv.Itoa(n), name).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
