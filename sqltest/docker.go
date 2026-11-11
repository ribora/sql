package sqltest

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const runtimeTimeout = 30 * time.Second

// Last resort, after Dsn and Env. No password exists to keep anywhere; a binary
// rather than testcontainers, to stay out of go.mod.
func Docker(image string, opts ...DockerOption) Option {
	return func(s *server) {
		kind := s.host.kind()
		c := &container{kind: kind, image: image, name: kind.name, port: kind.port}
		for _, opt := range opts {
			opt(c)
		}
		s.container = c
	}
}

// One container per repository and dialect: two services are two databases in
// it. Shared defaults, so every service lands in the same instance.
type containerKind struct {
	name       string
	port       int
	serverPort int
	noPassword string
	dsn        func(port int, database string) string
}

type DockerOption func(*container)

func Container(name string) DockerOption {
	return func(c *container) { c.name = name }
}

func Port(port int) DockerOption {
	return func(c *container) { c.port = port }
}

// For a machine with both docker and podman.
func Runtime(binary string) DockerOption {
	return func(c *container) { c.runtime = binary }
}

type container struct {
	kind    containerKind
	image   string
	name    string
	port    int
	runtime string
}

// Does not wait for the server to accept connections: that is the caller's ping
// loop, which has the driver.
func (c *container) ensure(ctx context.Context) error {
	binary, err := c.binary()
	if err != nil {
		return err
	}
	// A hung runtime would otherwise hang the suite with no output at all.
	ctx, cancel := context.WithTimeout(ctx, runtimeTimeout)
	defer cancel()

	switch running, found := c.state(ctx, binary); {
	case !found:
		return c.start(ctx, binary)
	case !running:
		if out, err := exec.CommandContext(ctx, binary, "start", c.name).CombinedOutput(); err != nil {
			return fmt.Errorf("cannot start the container %q: %w: %s", c.name, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func (c *container) start(ctx context.Context, binary string) error {
	// Loopback only: with no password, publishing on every interface would be an
	// open database on whatever network the laptop is on.
	out, err := exec.CommandContext(ctx, binary, "run", "-d",
		"--name", c.name,
		"-p", "127.0.0.1:"+strconv.Itoa(c.port)+":"+strconv.Itoa(c.kind.serverPort),
		"-e", c.kind.noPassword,
		c.image,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot start %s as %q on port %d: %w: %s",
			c.image, c.name, c.port, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *container) state(ctx context.Context, binary string) (running, found bool) {
	out, err := exec.CommandContext(ctx, binary, "inspect", "-f", "{{.State.Running}}", c.name).Output()
	if err != nil {
		return false, false
	}
	return strings.TrimSpace(string(out)) == "true", true
}

func (c *container) binary() (string, error) {
	candidates := []string{"docker", "podman"}
	if c.runtime != "" {
		candidates = []string{c.runtime}
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("sqltest: no container runtime found (%s), pass sqltest.Dsn or sqltest.Env instead",
		strings.Join(candidates, " or "))
}

func (c *container) dsn(database string) string {
	return c.kind.dsn(c.port, database)
}

var errNoDatabase = errors.New("sqltest: sqltest.Docker needs sqltest.Database to name the database")
