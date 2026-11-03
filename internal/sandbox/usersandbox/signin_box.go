package usersandbox

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// StartSignIn keeps the floor and listener ahead of the first browser navigation.
func (b *DockerBackend) StartSignIn(ctx context.Context, s SignInSpec, prepare SignInPrepare) (box SignInBox, err error) {
	if err = validateSignIn(s); err != nil {
		return box, err
	}
	if b.egressImage == "" {
		return box, ErrSignInNeedsTheFloor
	}
	defer func() {
		if err == nil {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		err = errors.Join(err, b.StopSignIn(cleanup, s.Session))
	}()
	if err = b.ensureImage(ctx, s.SidecarImage); err != nil {
		return box, err
	}
	pids, init := b.limits.PidsLimit, true
	name := "aura-" + s.Session
	owner, err := b.cli.ContainerCreate(ctx, client.ContainerCreateOptions{Name: name,
		Config:     &container.Config{Image: s.SidecarImage, Cmd: keepAliveCmd, Labels: signInLabels(s.Session, "sidecar")},
		HostConfig: &container.HostConfig{Init: &init, NanoCPUs: b.limits.NanoCPUs, Memory: b.limits.MemoryBytes, PidsLimit: &pids}})
	if err != nil {
		return box, fmt.Errorf("create sign-in sidecar: %w", err)
	}
	box = SignInBox{Session: s.Session, Sidecar: BoxHandle{Kind: Browser, ContainerID: owner.ID, IdentityID: s.IdentityID}}
	if _, err = b.cli.ContainerStart(ctx, owner.ID, client.ContainerStartOptions{}); err != nil {
		return box, fmt.Errorf("start sign-in sidecar: %w", err)
	}
	if err = b.startEgress(ctx, name+"-egress", owner.ID, s.Egress, signInLabels(s.Session, "egress")); err != nil {
		return box, err
	}
	if prepare != nil {
		if err = prepare(ctx, box); err != nil {
			return box, err
		}
	}
	desktop, err := b.cli.ContainerCreate(ctx, client.ContainerCreateOptions{Name: name + "-desktop",
		Config: &container.Config{Image: s.DesktopImage, Env: signInEnv(s), Labels: signInLabels(s.Session, "desktop")}, HostConfig: signInHostConfig(owner.ID)})
	if err != nil {
		return box, fmt.Errorf("create sign-in desktop: %w", err)
	}
	if _, err = b.cli.ContainerStart(ctx, desktop.ID, client.ContainerStartOptions{}); err != nil {
		return box, fmt.Errorf("start sign-in desktop: %w", err)
	}
	if err = b.waitSignInDesktop(ctx, box.Sidecar); err != nil {
		return box, err
	}
	return box, nil
}

func (b *DockerBackend) waitSignInDesktop(ctx context.Context, sidecar BoxHandle) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := b.Exec(ctx, sidecar, ExecRequest{Command: `node -e 'const s=require("node:net").connect(3000,"127.0.0.1",()=>process.exit(0));s.on("error",()=>process.exit(1));s.setTimeout(1000,()=>process.exit(1))'`})
		if err == nil && result.ExitCode == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("sign-in desktop did not become ready")
		case <-ticker.C:
		}
	}
}

// StopSignIn removes anonymous image volumes as well as all three containers.
func (b *DockerBackend) StopSignIn(ctx context.Context, session string) error {
	if !signInSessionPattern.MatchString(session) {
		return errors.New("sandbox: invalid sign-in session")
	}
	defer b.releaseSignInSlot(session)
	items, err := b.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", signInLabel+"="+session)})
	if err != nil {
		return err
	}
	return b.removeSignInContainers(ctx, items.Items)
}

// SweepSignIns retires logins whose in-memory callback state died with Aura.
func (b *DockerBackend) SweepSignIns(ctx context.Context) error {
	items, err := b.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", signInLabel)})
	if err != nil {
		return err
	}
	return b.removeSignInContainers(ctx, items.Items)
}

func (b *DockerBackend) removeSignInContainers(ctx context.Context, items []container.Summary) error {
	order := map[string]int{"desktop": 0, "egress": 1, "sidecar": 2}
	sort.SliceStable(items, func(i, j int) bool {
		return order[items[i].Labels[signInRoleLabel]] < order[items[j].Labels[signInRoleLabel]]
	})
	var failures []error
	for _, item := range items {
		if _, err := b.cli.ContainerRemove(ctx, item.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true}); err != nil && !cerrdefs.IsNotFound(err) {
			failures = append(failures, fmt.Errorf("remove sign-in container %s: %w", item.ID, err))
		}
		b.releaseSignInSlot(item.Labels[signInLabel])
	}
	return errors.Join(failures...)
}

func (b *DockerBackend) releaseSignInSlot(session string) {
	b.admission.Lock()
	defer b.admission.Unlock()
	delete(b.browserCalls, session)
}
