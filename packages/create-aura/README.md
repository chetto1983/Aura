# create-aura-appliance

Interactive installer for an [Aura](https://github.com/chetto1983/Aura) appliance — on this
machine, or on another one over SSH.

```bash
npx create-aura-appliance            # asks whether to install here or remotely
npx create-aura-appliance --mode remote
```

## What it does

The wizard collects the target, writes an installer config, and then
runs the appliance installer. The installer is **carried inside this package** as a
self-extracting archive: nothing is fetched from a source host at install time, so the
payload you run is the payload npm delivered.

The embedding backend is detected by the installer itself, running on the machine that will
actually run Aura rather than the one you are typing on. An NVIDIA GPU that Docker can drive
selects CUDA; otherwise a GPU exposing `/dev/dri` render nodes (Intel or AMD, integrated
included) selects Vulkan; otherwise embeddings run on the CPU. Apple's Metal is not reachable
from a Docker container on macOS, so a Mac runs embeddings on the CPU. You are not asked.

Installs made from this package track the `edge` channel: the appliance images carry the
moving `:edge` tag, Docker is set to always re-pull them, and a systemd timer applies new
images as they are published — so the box keeps itself current without you returning to it.

## Requirements

On the machine running this wizard: Node.js >= 22.13.

On the target: Linux (local installs may also be macOS), `x86_64` or `arm64`, at least
4 CPU cores, 14 GiB of usable RAM, 20 GiB of free disk, `sudo`, `curl` and `openssl`, and
network reach to `ghcr.io`, `get.docker.com` and `huggingface.co`. The hardware floors are
checked before anything is copied, and again by the installer itself.

The RAM floor is measured against `MemTotal`, which is installed RAM minus firmware, kernel
and integrated-GPU reservations — a 16 GB machine reports well under 16 GiB, which is why
the floor is where it is.

## License

MIT
