# Aleph0 Cloud Ecosystem — architecture (MVP)

Our own cloud platform built on **OpenStack**, with a custom portal and API.
Customers never talk to OpenStack directly: they only use our portal and our API.

---

## 1. What we offer first

| Service | Implementation | Notes |
|---|---|---|
| **VMs** | Nova (KVM) | SSH keys, OS images, web console, resize, snapshots |
| **Storage** | Cinder on Ceph RBD + Ceph RGW (S3) | attachable volumes + S3-compatible object storage |
| **Virtual networks** | Neutron with OVN | private networks, routers/NAT, floating IPs, security groups |
| **Load balancer** | Octavia with the OVN provider | L4 only (TCP/UDP) at first; L7/HTTPS (amphora) later |

Included by default: scheduled backups (snapshots), basic per-VM metrics, per-organization quotas.

---

## 2. Architecture

```
            Users (browser)
                    │ HTTPS
┌───────────────────▼────────────────────────────┐
│ GCP (EU region)                                │
│  • React frontend  (Cloud Storage + CDN)       │
│  • Go backend      (Cloud Run)                 │
│  • Job worker      (Cloud Run / small VM)      │
│  • Keycloak        (authentication)            │
│  • Cloud SQL       (DBs: keycloak, api)        │
└───────────────────┬────────────────────────────┘
                    │ site-to-site VPN
┌───────────────────▼────────────────────────────┐
│ Own infrastructure — N nodes (3 at launch)     │
│  • OpenStack (Kolla-Ansible)                   │
│  • Ceph (cephadm)                              │
└────────────────────────────────────────────────┘

Publicly exposed from our own infrastructure (only these):
  • Ceph RGW (S3)   → s3.<domain>
  • noVNC proxy     → VM consoles (or proxied through the backend)
```

### Principles

- **OpenStack is the source of truth** for resources. Our DB does not duplicate VMs or volumes.
- **OpenStack APIs stay private**; the only client is the Go backend, over the VPN.
- **Keycloak authenticates the platform only.** No Keystone federation.
- The backend uses a **service user** in Keystone with the `member` role (not `admin`) on each project.
  Each organization = one OpenStack project.
- On every request: check organization → project in our DB, then a Keystone token
  **scoped to that organization's project**.
- There is always a **local admin account in Keystone** (not federated) for emergencies.

### API

The API is a product: customers use it directly, not only through the portal.

- **Spec-first:** `api/openapi.yaml` is the contract. Go server code (`oapi-codegen`) and the
  frontend client (`orval`: TanStack Query hooks + Zod schemas) are generated from it.
- Requests to routes in the spec are validated against it (`nethttp-middleware`, using the
  embedded spec) before a handler runs; violations return a 400 problem response.
- Errors use RFC 9457 problem details (`application/problem+json`).
- Every endpoint under `/v1/` requires a Keycloak access token whose `aud` contains `api`.

---

## 3. Dev environment

The dev machine is a Windows 11 PC; all development happens in **WSL2 (Ubuntu)**, with the repo
in the WSL filesystem (`~/projects/`), not under `/mnt/c`. Kolla-Ansible and Terraform with
libvirt need Linux, and CI and production are Linux too.

The cluster is described by a node file (`deploy/nodes/<env>.yml`): each node has roles
(`control`, `network`, `compute`, `storage`), and nodes can be added or removed while the
cluster runs. In dev, nodes are libvirt/KVM VMs inside WSL2; in production, the same scripts
run against physical machines. See `deploy/README.md`.

### Host requirements

- 32 GB RAM, 8+ CPU cores
- SSD/NVMe with ~300 GB free
- Nested virtualization available in WSL2 (`/dev/kvm` present)
- `C:\Users\<user>\.wslconfig` with `memory=24GB` (WSL gets half of the RAM by default)

```bash
# nested virtualization check inside WSL (Y = enabled)
cat /sys/module/kvm_intel/parameters/nested   # or kvm_amd
```

### Dev nodes

The default `deploy/nodes/dev.yml`; edit it to change the layout.

| Node | Roles | vCPU | RAM | Disks |
|---|---|---|---|---|
| node1 | control, network, compute, storage | 4 | 10 GB | 40 GB system + 50 GB Ceph |
| node2 | compute, storage | 4 | 4 GB | 40 GB system + 50 GB Ceph |
| node3 | compute, storage | 4 | 4 GB | 40 GB system + 50 GB Ceph |

- Ubuntu 24.04 nodes, CPU mode `host-passthrough`
- Networks: management 10.10.0.0/24 (API VIP 10.10.0.10), storage 10.20.0.0/24, external
  10.30.0.0/24 (floating IPs .100–.199)
- Tenant VMs run at the third nesting level (Windows → WSL2 → node → VM), so dev uses
  `nova_compute_virt_type: qemu`
- node1 fits in 10 GB because dev runs one worker per OpenStack service
  (`openstack_service_workers` and `openstack_service_rpc_workers` = 1) and VM nodes get a 2 GB
  swap file; Ceph OSDs are capped with `ceph_osd_memory_target` (1.5 GB)
- With one OSD per node, the default pools exceed Ceph's 250 PGs per OSD and the cluster sits in
  `HEALTH_WARN`, so dev sets `ceph_mon_max_pg_per_osd: 400`
- Horizon is disabled (we have our own portal); Octavia is enabled with the load balancer step

### Dev vs production

| | Dev | Production |
|---|---|---|
| Control nodes | 1, behind a VIP (HAProxy + keepalived) | 3 or more, odd (HA: HAProxy + keepalived, Galera, RabbitMQ cluster) |
| Ceph `osd_memory_target` | ~1.5 GB | default (4 GB) |
| OpenStack images | Kolla's prebuilt quay.io test images | our own build |
| Networks | virtual bridges / VLANs | separate physical interfaces, storage ≥ 10 GbE |
| Keycloak, PostgreSQL | local, `docker compose` | GCP |
| Nova virt type | `qemu` | `kvm` |

---

## 4. Tools

| Tool | For |
|---|---|
| Terraform (`libvirt` provider) | creating the dev VMs |
| cephadm | installing Ceph |
| Kolla-Ansible + Ansible | installing OpenStack; our playbooks prepare hosts, Ceph and the initial resources |
| OpenStack CLI (`python-openstackclient`) | initial setup and debugging |
| Docker + Docker Compose | local Keycloak and PostgreSQL |
| Go | backend; `oapi-codegen` (and later `goose`, `sqlc`) pinned as `go tool` entries in `api/go.mod` |
| Node.js | frontend; `orval` pinned in `web/package.json` |

---

## 5. Repo layout

Folders are created by the issue that first needs them.

```
.
├── README.md
├── Makefile                 # make up | dev | generate | test | down | reset
├── docker-compose.yml       # local: PostgreSQL + Keycloak
├── .env.example
│
├── api/                     # Go backend (modular monolith)
│   ├── openapi.yaml         # API contract (source of truth)
│   ├── cmd/api/main.go      # api serve | api migrate | api worker
│   ├── internal/
│   │   ├── config/
│   │   ├── server/          # router, middleware
│   │   ├── auth/            # Keycloak JWT verification
│   │   ├── problem/         # RFC 9457 error responses
│   │   ├── oapi/            # generated from openapi.yaml
│   │   ├── handlers/        # REST endpoints
│   │   ├── openstack/       # Gophercloud v2: compute, network, volume, image, lb
│   │   ├── jobs/            # async operations
│   │   ├── metering/        # (reserved) usage metering
│   │   ├── billing/         # (reserved) Stripe
│   │   └── db/{queries,gen}
│   └── migrations/
│
├── web/                     # React + Vite + TypeScript
│   └── src/{auth,api,components,routes}
│
├── deploy/
│   ├── postgres/            # local DB init
│   ├── keycloak/            # exported realm
│   ├── infra.sh             # apply | remove | destroy | stop | start | reconfigure | test
│   ├── nodes/               # node files (one per env) + role groups
│   ├── playbooks/           # hosts, Ceph, OpenStack init, node removal, start
│   ├── terraform/           # dev VMs from the node file
│   └── kolla/               # services.ini + <env>/globals.yml
│
└── docs/architecture.md
```

### Frontend stack

React + Vite + TypeScript, TanStack Router, TanStack Query, Zod, shadcn/ui (base-nova style: Tailwind CSS v4 + Base UI).
Login: OIDC Authorization Code + PKCE against Keycloak (`oidc-client-ts`). Zustand is added when
there is client-side UI state to share.

---

## 6. Getting started (dev)

The app stack (portal, API, Keycloak, PostgreSQL) is described in `README.md`, the
infrastructure in `deploy/README.md`.

### 6.1 Infrastructure

```bash
make infra-apply      # VMs, Ceph, OpenStack and the initial resources, from deploy/nodes/dev.yml
make infra-stop       # shut the dev VMs down and free most of their RAM
make infra-start      # bring them back, and repair what a boot leaves broken
make infra-destroy    # wipe the dev cluster
```

Requirements, adding and removing nodes, stopping and starting, troubleshooting:
`deploy/README.md`. Main Kolla settings (`deploy/kolla/dev/globals.yml`):

```yaml
kolla_base_distro: "ubuntu"
kolla_internal_vip_address: "{{ vip_address }}"   # dedicated VIP, from the node file
enable_haproxy: "yes"                             # from day one, so controllers can join
neutron_plugin_agent: "ovn"
nova_compute_virt_type: "qemu"                    # dev (nested); production: "kvm"
glance_backend_ceph: "yes"
cinder_backend_ceph: "yes"
nova_backend_ceph: "yes"
enable_horizon: "no"
enable_octavia: "no"                              # "yes" with the load balancer step
openstack_service_workers: "1"                    # dev only: fits node1 in 10 GB
openstack_service_rpc_workers: "1"                # dev only
kolla_test_images: "yes"                          # dev only: Kolla's prebuilt quay.io images
```

Versions: Ceph is pinned to 20.2.0 to match the Ceph client libraries inside Kolla's 2026.2
images. 20.2.4 monitors require the `aes256k` cephx cipher, which those clients cannot read, so
upgrade Ceph together with Kolla's images. 20.2.0 lacks 20.2.4's cephx security fix.

### 6.2 Initial OpenStack setup

Created by `make infra-apply`: external network `public` with floating IPs, flavors `s.tiny`
and `s.small`, CirrOS, project `dev` with a network and router, and the backend service user
`aleph0-api` (`member` on `dev`). Kolla's `admin` is the local emergency admin (not federated).
Later: Ubuntu and Debian images, default per-project quotas.

---

## 7. Database (minimum)

PostgreSQL, separate from OpenStack's MariaDB. Tables arrive with the issues that need them
(migrations with goose, queries with sqlc).

| Table | Purpose |
|---|---|
| `organizations` | org ↔ Keycloak group ↔ OpenStack project (+ `stripe_customer_id` later) |
| `memberships` | user ↔ organization ↔ role (admin / member) |
| `jobs` | async operations (provisioning, deletions) and their status |
| `resource_events` | who, what, when: create / resize / delete. Basis for audit, analytics, billing |

---

## 8. Security and abuse (minimum)

- Secrets only in `.env` / Secret Manager, never in git
- Port 25 blocked by default for customer VMs (anti-spam)
- Creation limits and quotas per organization
- Monitoring of abnormal CPU usage (crypto mining)
- Terms of service + GDPR before the first customers

---

## 9. Production notes

Starts with 3 physical nodes, each with all four roles; more nodes are added to the node file.

- HA control plane on 3 controllers (odd count); the cluster tolerates losing **one** of them
- Ceph with `ceph_pool_size: 3` (one copy per host)
- Max load ~(N−1)/N of capacity, so a failed node's VMs fit on the others
- Server groups with **anti-affinity** for customers who want redundancy
- VM HA: evacuation driven by the backend, or **Masakari** (later)

---

## 10. Open questions

Raised during planning, to be settled in the step that needs them.

- **Creating projects:** a service user with only the `member` role cannot create OpenStack
  projects. Onboarding a new organization needs a separate identity or a custom policy.
- **VPN:** GCP Cloud VPN supports IPsec only (no WireGuard), and Cloud Run needs Direct VPC egress
  (or a connector) to reach the VPN.
- **Production domain layout:** portal and API under one domain (no CORS) via the GCP load balancer,
  or separate origins.
- **Kolla images and Ceph versions:** production builds its own Kolla images (dev uses Kolla's
  prebuilt test images). Keep the Ceph version matched to the client libraries in those images:
  today Ceph is pinned to 20.2.0, because 20.2.4 monitors require a cephx cipher the clients
  cannot read, and 20.2.0 lacks 20.2.4's cephx security fix.
- **Neutron's stale start-time file:** a node rebooted outside `make infra-start` can leave
  `neutron_server` unable to register again (`/tmp/neutron_start_time*` survives a container
  restart). Find a durable fix: a tmpfs `/tmp` override for the container, or an upstream patch.
- **Image registry:** pulls from quay.io reset connections often. A local registry mirror would
  remove that dependency.

---

## 11. Outside the MVP (roadmap)

- Masakari (automatic VM restart)
- Octavia amphora (L7 load balancer / TLS termination)
- Metering (own collector, or Ceilometer + CloudKitty)
- Stripe payments (usage-based billing + webhooks)
- Analytics
- PaaS services (managed databases, Kubernetes)
- Managed DNS (Designate)

---

## 12. Glossary

| Abbreviation | Meaning |
|---|---|
| **API** | Application Programming Interface — how programs talk to each other |
| **CDN** | Content Delivery Network — serves static files fast |
| **GCP** | Google Cloud Platform |
| **HA** | High Availability — the service keeps working when a component fails |
| **JWT** | JSON Web Token — signed token issued by Keycloak |
| **KVM** | Kernel-based Virtual Machine — the Linux kernel hypervisor |
| **L4 / L7** | transport layer (TCP/UDP) / application layer (HTTP) |
| **MVP** | Minimum Viable Product — the first usable version |
| **NAT** | Network Address Translation — internet access from private networks |
| **noVNC** | browser VNC client, for VM consoles |
| **OIDC** | OpenID Connect — the authentication standard Keycloak uses |
| **OSD** | Object Storage Daemon — the Ceph process that manages one disk |
| **OVN** | Open Virtual Network — the virtual network backend for Neutron |
| **PaaS** | Platform as a Service — managed services on top of the infrastructure |
| **PKCE** | Proof Key for Code Exchange — secure OIDC login from the browser |
| **RBD** | RADOS Block Device — virtual disks stored in Ceph |
| **RGW** | RADOS Gateway — S3 API on top of Ceph |
| **S3** | Simple Storage Service — the standard object storage API |
| **vCPU** | virtual CPU assigned to a VM |
| **VIP** | Virtual IP — an IP address that moves between nodes |
| **VPN** | Virtual Private Network — encrypted tunnel between networks |
| **WSL** | Windows Subsystem for Linux — runs a Linux VM on Windows |
