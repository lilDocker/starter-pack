# lildocker 🐋

A fully custom container runtime written in Go — with its own image format, layered overlay filesystem, networking, volumes, and a custom `lilDockfile` build syntax.

Built from scratch using only Linux kernel primitives: namespaces, cgroups, overlayfs, veth pairs, and iptables.

---

## Features

| Feature | Status |
|---|---|
| Custom `lilDockfile` syntax | ✅ |
| Layered image builds (overlayfs) | ✅ |
| Run containers with namespace isolation | ✅ |
| cgroup resource limits (memory, CPU) | ✅ |
| Container networking (bridge + veth) | ✅ |
| Volume mounts (bind mounts) | ✅ |
| Port forwarding (iptables DNAT) | ✅ |
| Exec into running containers (nsenter) | ✅ |
| Container logs | ✅ |

---

## Quick Start

### 1. Build the binary

```bash
make build
```

### 2. Bootstrap a base image (Alpine Linux)

```bash
make bootstrap
# or: sudo ./bootstrap.sh
```

This downloads Alpine Linux minirootfs and imports it as `alpine:latest`.

### 3. Write a `lilDockfile`

```
FROM alpine:latest

WORKDIR /app

ENV APP_PORT=8080

RUN apk add --no-cache curl

COPY myapp /app/myapp

EXPOSE 8080

CMD ["/app/myapp"]
```

### 4. Build your image

```bash
sudo lildocker build -t myapp:latest .
```

### 5. Run a container

```bash
# Foreground
sudo lildocker run myapp:latest

# With port mapping and volume
sudo lildocker run -p 8080:8080 -v /data:/app/data myapp:latest

# With resource limits
sudo lildocker run -m 256m --cpu-shares 512 myapp:latest

# Background (detached)
sudo lildocker run -d --name mycontainer myapp:latest
```

---

## lilDockfile Reference

The `lilDockfile` is lildocker's custom build file format.

### Instructions

| Instruction | Syntax | Description |
|---|---|---|
| `FROM` | `FROM <image>[:<tag>]` | Base image. Must be first. |
| `WORKDIR` | `WORKDIR <path>` | Set working directory |
| `COPY` | `COPY <src> <dst>` | Copy from build context |
| `ENV` | `ENV KEY=VALUE` | Set environment variable |
| `RUN` | `RUN <command>` | Run a shell command (new layer) |
| `EXPOSE` | `EXPOSE <port>[/tcp\|udp]` | Document a port |
| `CMD` | `CMD ["exec", "arg"]` | Default start command |
| `LABEL` | `LABEL key=value` | Metadata |
| `ARG` | `ARG NAME[=default]` | Build-time variable |
| `VOLUME` | `VOLUME /path` | Declare a mount point |
| `USER` | `USER name` | Set runtime user |

### Comments and line continuation

```
# This is a comment
RUN apk add \
    curl \
    bash
```

---

## CLI Reference

```
lildocker build -t <name[:tag]> [path]   Build an image from a lilDockfile
lildocker run [options] <image> [cmd]    Run a container
lildocker images                         List images
lildocker ps                             List containers
lildocker stop <id>                      Stop a container
lildocker rm <id>                        Remove a container
lildocker rmi <image>                    Remove an image
lildocker exec <id> <cmd>                Exec in container
lildocker logs <id>                      View logs
lildocker network ls                     List networks
lildocker network create <name>          Create a network
```

### Run options

```
-d                 Detach (background)
--name <name>      Container name
-h <hostname>      Hostname
-p host:ctr        Port mapping (repeatable)
-v host:ctr[:ro]   Volume mount (repeatable)
-e KEY=VAL         Environment variable (repeatable)
-m 256m            Memory limit (k/m/g suffix)
--cpu-shares 512   CPU shares
--net <network>    Connect to network
```

---

## Storage Layout

```
/var/lib/lildocker/
├── images/
│   └── <image-id>/
│       ├── manifest.json
│       └── layers/
│           ├── <layer-id>/     ← read-only layer (diff)
│           └── <layer-id>/
├── containers/
│   └── <container-id>/
│       ├── config.json
│       ├── upper/              ← container's writable layer
│       ├── work/               ← overlayfs workdir
│       ├── merged/             ← unified mount point (rootfs)
│       └── logs/
│           └── stdout.log
└── networks/
    └── <network-id>.json
```

---

## How Layering Works

lildocker uses Linux **overlayfs** for image layers, exactly like real Docker:

```
Layer 3 (RUN apk add curl)   ← uppermost, written last
Layer 2 (COPY myapp /app)
Layer 1 (FROM alpine)        ← bottommost, base image
─────────────────────────────
Merged view = union of all   ← what the container sees
```

Each `RUN`, `COPY`, and `WORKDIR` creates a new read-only layer. When a container runs, a fresh writable layer (`upper`) sits on top — so the image is never modified.

---

## Networking

```
Host                         Container
──────────────────────────────────────────
lil0 bridge (172.20.x.1)
  └── veth pair ──────────── eth0 (172.20.x.2)
                             default gw → 172.20.x.1
iptables MASQUERADE          → outbound internet
iptables DNAT (-p 8080:80)   → port forwarding
```

Create a network and connect containers:

```bash
sudo lildocker network create mynet
sudo lildocker run --net mynet -p 8080:80 myapp:latest
```

---

## Requirements

- **Linux** (kernel 4.x+) — namespaces and overlayfs are Linux-only
- **Root / sudo** — namespace creation requires elevated privileges  
- **Go 1.21+** for building
- **iproute2** (`ip` command) for networking
- **iptables** for NAT and port forwarding
- **nsenter** for `exec` into running containers

---

## Project Structure

```
lildocker/
├── main.go                    ← CLI entry point
├── cmd/
│   └── commands.go            ← Command handlers
├── internal/
│   ├── parser/
│   │   └── parser.go          ← lilDockfile parser
│   ├── builder/
│   │   └── builder.go         ← Image builder
│   ├── image/
│   │   └── store.go           ← Image/layer storage
│   ├── container/
│   │   └── container.go       ← Container runtime
│   └── network/
│       └── network.go         ← Virtual networking
├── example/
│   └── lilDockfile            ← Example build file
├── bootstrap.sh               ← Import Alpine base image
└── Makefile
```
