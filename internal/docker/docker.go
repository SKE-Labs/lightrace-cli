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
	for _, n := range networks {
		if n.Name == name {
			return nil
		}
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
	return renderPullProgress(reader, img)
}

type RunConfig struct {
	ProjectID    string
	Service      string
	Image        string
	Env          []string
	Ports        map[string]string // container port -> host port
	Volumes      map[string]string // source -> container path (named volume or host path)
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

// RunOnce starts a container, streams its output, waits for it to exit,
// removes it, and returns the exit code.
func RunOnce(ctx context.Context, rc RunConfig) (int, error) {
	c, err := Client()
	if err != nil {
		return -1, err
	}

	name := ContainerName(rc.ProjectID, rc.Service)
	_ = c.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})

	containerConfig := &container.Config{
		Image:  rc.Image,
		Env:    rc.Env,
		Labels: map[string]string{LabelProject: rc.ProjectID},
	}
	if len(rc.Cmd) > 0 {
		containerConfig.Cmd = rc.Cmd
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

	resp, err := c.ContainerCreate(ctx, containerConfig, &container.HostConfig{}, networkConfig, nil, name)
	if err != nil {
		return -1, fmt.Errorf("creating %s: %w", name, err)
	}

	defer func() {
		_ = c.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	if err := c.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return -1, fmt.Errorf("starting %s: %w", name, err)
	}

	// Stream logs
	logs, err := c.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err == nil {
		_, _ = io.Copy(os.Stdout, logs)
		logs.Close()
	}

	// Wait for exit
	waitCh, errCh := c.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case result := <-waitCh:
		return int(result.StatusCode), nil
	case err := <-errCh:
		return -1, err
	}
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

func PgVolumeName(projectID string) string {
	return projectID + "_pgdata"
}

func RedisVolumeName(projectID string) string {
	return projectID + "_redisdata"
}

func RemoveVolume(ctx context.Context, name string) error {
	c, err := Client()
	if err != nil {
		return err
	}
	return c.VolumeRemove(ctx, name, true)
}

func RemoveNetwork(ctx context.Context, projectID string) error {
	c, err := Client()
	if err != nil {
		return err
	}

	// Find networks by label to clean up both old and new naming patterns.
	networks, err := c.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", LabelProject+"="+projectID)),
	})
	if err != nil {
		return err
	}

	for _, n := range networks {
		if err := c.NetworkRemove(ctx, n.ID); err != nil {
			return err
		}
	}
	return nil
}
