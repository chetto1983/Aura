//go:build docker_integration

package usersandbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func signInDocker(t *testing.T) (context.Context, *DockerBackend, SignInSpec) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(cancel)
	cli := newTestDockerClient(t)
	if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
		t.Fatalf("Docker required: %v", err)
	}
	sandbox := config.LoadDB().Sandbox
	image := sandbox.SignInBrowserImage
	if _, err := cli.ImageInspect(ctx, image); err != nil {
		t.Fatalf("pull the desktop image before this tier: %v", err)
	}
	sidecar := sandbox.Image
	var id [12]byte
	_, _ = rand.Read(id[:])
	session := "signin-" + hex.EncodeToString(id[:])
	s := SignInSpec{Session: session, IdentityID: "test-" + session, DesktopImage: image, SidecarImage: sidecar, AuthURL: "https://example.com",
		CockpitOrigin: "https://aura.test:8443", StreamOrigin: "https://aura.test:8444", Egress: EgressPolicy{Floor: true}}
	b := NewDockerBackend(cli, sidecar, Resources{NanoCPUs: 1_000_000_000, MemoryBytes: 512 << 20, PidsLimit: 256}, WithEgress(egressTestImage()))
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if err := b.StopSignIn(cleanup, session); err != nil {
			t.Error(err)
		}
	})
	return ctx, b, s
}

func TestSignInPreparesCallbackBehindFloorBeforeDesktop(t *testing.T) {
	ctx, b, s := signInDocker(t)
	prepared := false
	_, err := b.StartSignIn(ctx, s, func(ctx context.Context, box SignInBox) error {
		floor, err := b.cli.ContainerInspect(ctx, "aura-"+s.Session+"-egress", client.ContainerInspectOptions{})
		if err != nil {
			return err
		}
		probe, err := b.Exec(ctx, BoxHandle{ContainerID: floor.Container.ID}, ExecRequest{Command: "nft list table ip aura_egress >/dev/null"})
		if err != nil || probe.ExitCode != 0 {
			return errors.New("callback preparation preceded the floor")
		}
		desktops, err := b.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).
			Add("label", signInLabel+"="+s.Session).Add("label", signInRoleLabel+"=desktop")})
		if err != nil {
			return err
		}
		if len(desktops.Items) != 0 {
			return errors.New("desktop existed before callback preparation")
		}
		if box.Sidecar.ContainerID == "" {
			return errors.New("preparation received no sidecar")
		}
		prepared = true
		return nil
	})
	if err != nil || !prepared {
		t.Fatalf("callback preparation order: %v", err)
	}
}

func TestSignInRollbackRemovesItsPreparedResources(t *testing.T) {
	for _, phase := range []string{"listener", "desktop"} {
		t.Run(phase, func(t *testing.T) {
			ctx, b, s := signInDocker(t)
			failure := errors.New("synthetic listener failure")
			if phase == "desktop" {
				s.DesktopImage = "aura-signin-test-missing-image:latest"
			}
			_, err := b.StartSignIn(ctx, s, func(context.Context, SignInBox) error {
				if phase == "listener" {
					return failure
				}
				return nil
			})
			if err == nil || (phase == "listener" && !errors.Is(err, failure)) {
				t.Fatalf("startup failure was lost: %v", err)
			}
			items, listErr := b.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", signInLabel+"="+s.Session)})
			if listErr != nil || len(items.Items) != 0 {
				t.Fatalf("rollback left %d containers: %v", len(items.Items), listErr)
			}
		})
	}
}

func TestSignInBoxStartsBehindTheFloor(t *testing.T) {
	ctx, b, s := signInDocker(t)
	box, err := b.StartSignIn(ctx, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	// No page or credentials are read: these are transport probes inside the netns.
	res, err := b.Exec(ctx, box.Sidecar, ExecRequest{Command: `node -e 'const s=require("node:net").connect(3000,"127.0.0.1",()=>process.exit(0));s.on("error",()=>process.exit(1));s.setTimeout(2000,()=>process.exit(1))'`})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("desktop stream not ready: exit %d, %v", res.ExitCode, err)
	}
	inspect, err := b.cli.ContainerInspect(ctx, box.Sidecar.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var gateway string
	for _, network := range inspect.Container.NetworkSettings.Networks {
		if network.Gateway.IsValid() && network.Gateway.IsPrivate() {
			gateway = network.Gateway.String()
			break
		}
	}
	if gateway == "" {
		t.Fatal("no bridge gateway to probe")
	}
	res, err = b.Exec(ctx, box.Sidecar, ExecRequest{Env: []string{"PROBE_TARGET=" + gateway}, Command: `node -e 'const s=require("node:net").connect(80,process.env.PROBE_TARGET,()=>process.exit(1));s.on("error",e=>process.exit(e.code==="ECONNREFUSED"?1:0));s.setTimeout(1500,()=>process.exit(0))'`})
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("private bridge was reachable or refused outside the floor: exit %d, %v", res.ExitCode, err)
	}
}

func TestStopSignInLeavesNothing(t *testing.T) {
	ctx, b, s := signInDocker(t)
	if _, err := b.StartSignIn(ctx, s, nil); err != nil {
		t.Fatal(err)
	}
	inspect, err := b.cli.ContainerInspect(ctx, "aura-"+s.Session+"-desktop", client.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var volume string
	for _, m := range inspect.Container.Mounts {
		if m.Destination == "/config" {
			volume = m.Name
		}
	}
	if volume == "" {
		t.Fatal("the real image did not allocate its anonymous /config volume")
	}
	if err = b.StopSignIn(ctx, s.Session); err != nil {
		t.Fatal(err)
	}
	items, err := b.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", signInLabel+"="+s.Session)})
	if err != nil || len(items.Items) != 0 {
		t.Fatalf("sign-in containers survived: %d, %v", len(items.Items), err)
	}
	if _, err = b.cli.VolumeInspect(ctx, volume, client.VolumeInspectOptions{}); !cerrdefs.IsNotFound(err) {
		t.Fatalf("anonymous login profile removal not confirmed: %v", err)
	}
}

func TestSweepRemovesEverySignInContainerLeftBehind(t *testing.T) {
	ctx, b, s := signInDocker(t)
	if _, err := b.StartSignIn(ctx, s, nil); err != nil {
		t.Fatal(err)
	}
	var controls []string
	for _, kind := range []string{"shell", "browser"} {
		created, err := b.cli.ContainerCreate(ctx, client.ContainerCreateOptions{Name: "aura-" + kind + "-control-" + s.Session, Config: &container.Config{Image: s.SidecarImage, Cmd: keepAliveCmd}})
		if err != nil {
			t.Fatal(err)
		}
		controls = append(controls, created.ID)
		t.Cleanup(func() {
			_, _ = b.cli.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
		})
	}
	if err := b.SweepSignIns(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := b.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", signInLabel+"="+s.Session)})
	if err != nil || len(items.Items) != 0 {
		t.Fatalf("leftovers survived sweep: %d, %v", len(items.Items), err)
	}
	for _, id := range controls {
		if _, err := b.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{}); err != nil {
			t.Errorf("unrelated %s was removed: %v", id, err)
		}
	}
}
