package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

const LabelProject = "io.lightrace.project"

var cli *client.Client

func Client() (*client.Client, error) {
	if cli != nil {
		return cli, nil
	}
	var err error
	cli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("cannot connect to Docker: %w\nIs Docker running?", err)
	}
	return cli, nil
}

func NetworkName(projectID string) string {
	return fmt.Sprintf("%s_net", projectID)
}

func ContainerName(projectID, service string) string {
	return fmt.Sprintf("%s-%s", projectID, service)
}

func EnsureNetwork(ctx context.Context, projectID string) error {
	c, err := Client()
	if err != nil {
		return err
	}

	name := NetworkName(projectID)
	networks, err := c.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return err
	}
	if len(networks) > 0 {
		return nil
	}

	_, err = c.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{LabelProject: projectID},
	})
	return err
}

func PullImage(ctx context.Context, img string) error {
	c, err := Client()
	if err != nil {
		return err
	}

	reader, err := c.ImagePull(ctx, img, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling %s: %w", img, err)
	}
	defer reader.Close()
	_, _ = io.Copy(os.Stdout, reader)
	return nil
}

type RunConfig struct {
	ProjectID    string
	Service      string
	Image        string
	Env          []string
	Ports        map[string]string // container port -> host port
	Volumes      map[string]string // host path -> container path
	HealthCmd    []string
	NetworkName  string
	NetworkAlias string
	Cmd          []string
}

func RunContainer(ctx context.Context, rc RunConfig) (string, error) {
	c, err := Client()
	if err != nil {
		return "", err
	}

	name := ContainerName(rc.ProjectID, rc.Service)

	_ = c.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})

	portBindings := nat.PortMap{}
	exposedPorts := nat.PortSet{}
	for containerPort, hostPort := range rc.Ports {
		cp := nat.Port(containerPort)
		exposedPorts[cp] = struct{}{}
		portBindings[cp] = []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: hostPort}}
	}

	var healthcheck *container.HealthConfig
	if len(rc.HealthCmd) > 0 {
		healthcheck = &container.HealthConfig{
			Test:        append([]string{"CMD-SHELL"}, rc.HealthCmd...),
			Interval:    5 * time.Second,
			Timeout:     3 * time.Second,
			Retries:     10,
			StartPeriod: 10 * time.Second,
		}
	}

	var binds []string
	for hostPath, containerPath := range rc.Volumes {
		binds = append(binds, fmt.Sprintf("%s:%s", hostPath, containerPath))
	}

	containerConfig := &container.Config{
		Image:        rc.Image,
		Env:          rc.Env,
		ExposedPorts: exposedPorts,
		Labels:       map[string]string{LabelProject: rc.ProjectID},
		Healthcheck:  healthcheck,
	}
	if len(rc.Cmd) > 0 {
		containerConfig.Cmd = rc.Cmd
	}

	hostConfig := &container.HostConfig{
		PortBindings:  portBindings,
		Binds:         binds,
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
	}

	networkConfig := &network.NetworkingConfig{}
	if rc.NetworkName != "" {
		alias := rc.NetworkAlias
		if alias == "" {
			alias = rc.Service
		}
		networkConfig.EndpointsConfig = map[string]*network.EndpointSettings{
			rc.NetworkName: {Aliases: []string{alias}},
		}
	}

	resp, err := c.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, name)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", name, err)
	}

	if err := c.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", fmt.Errorf("starting %s: %w", name, err)
	}

	return resp.ID, nil
}

func WaitHealthy(ctx context.Context, projectID, service string, timeout time.Duration) error {
	c, err := Client()
	if err != nil {
		return err
	}

	name := ContainerName(projectID, service)

	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = timeout
	b.InitialInterval = 1 * time.Second

	return backoff.Retry(func() error {
		inspect, err := c.ContainerInspect(ctx, name)
		if err != nil {
			return fmt.Errorf("inspecting %s: %w", name, err)
		}

		if inspect.State.Health == nil {
			if inspect.State.Running {
				return nil
			}
			return fmt.Errorf("%s is not running", name)
		}

		switch inspect.State.Health.Status {
		case "healthy":
			return nil
		case "unhealthy":
			return backoff.Permanent(fmt.Errorf("%s is unhealthy", name))
		default:
			return fmt.Errorf("%s health: %s", name, inspect.State.Health.Status)
		}
	}, backoff.WithContext(b, ctx))
}

func StopContainers(ctx context.Context, projectID string) error {
	c, err := Client()
	if err != nil {
		return err
	}

	containers, err := c.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", LabelProject+"="+projectID)),
	})
	if err != nil {
		return err
	}

	for _, ctr := range containers {
		fmt.Printf("  Stopping %s...\n", ctr.Names[0])
		timeout := 10
		_ = c.ContainerStop(ctx, ctr.ID, container.StopOptions{Timeout: &timeout})
		_ = c.ContainerRemove(ctx, ctr.ID, container.RemoveOptions{})
	}

	return nil
}

func RemoveNetwork(ctx context.Context, projectID string) error {
	c, err := Client()
	if err != nil {
		return err
	}
	return c.NetworkRemove(ctx, NetworkName(projectID))
}
