# Local infra: OpenStack + Ceph on a scalable set of nodes — design

Issue: #2. Builds and grows a local OpenStack + Ceph cluster from a node file. Nodes with any
roles can be added, and compute/storage nodes removed, while the cluster runs. The same scripts
serve the dev VMs (libvirt inside WSL2) and, later, physical machines; only creating the machine
differs.

## Versions

| Component | Version | Why |
|---|---|---|
| Node OS | Ubuntu 24.04 (Noble) cloud image | the only Ubuntu release Kolla-Ansible 2026.2 supports |
| Kolla-Ansible | `stable/2026.2`, installed from git into the venv | latest stable; `kolla_base_distro: ubuntu` to match the hosts |
| Ceph | Tentacle (20.2.x), cephadm from download.ceph.com | latest stable; Noble's archive still ships Squid's cephadm |
| Deploy venv Python | 3.12, managed by `uv` | the dev host is Ubuntu 26.04 with Python 3.14, outside Kolla's tested runtimes; 3.12 is Noble's default |
| Terraform | pinned binary, provider `dmacvicar/libvirt` 0.9.x pinned in `.terraform.lock.hcl` | already chosen in `docs/architecture.md` |

The deploy machine is WSL (`Ubuntu-26.04`), where `make` runs. Kolla's `deployment` group is
`localhost`.

## Layout

```
deploy/
├── README.md              # requirements, commands, adding/removing nodes, manual controller removal
├── requirements.txt       # ansible-core, kolla-ansible (git, stable/2026.2), openstacksdk
├── apply.sh               # infra-apply steps
├── node-remove.sh
├── nodes/
│   ├── dev.yml            # the dev cluster
│   ├── roles.yml          # constructed plugin: node_roles → role_* groups
│   └── testdata/          # fixture node files for infra-test
├── kolla/
│   ├── services.ini       # Kolla's service → group mapping, pointed at role_* groups
│   └── dev/
│       ├── globals.yml
│       ├── passwords.yml  # generated, gitignored
│       └── config/        # Ceph keyrings + ceph.conf for glance/cinder/nova, generated, gitignored
├── playbooks/
│   ├── validate.yml       # rules + orphan check
│   ├── pending.yml        # reads markers, writes the pending list
│   ├── hosts.yml          # /etc/hosts on all nodes, chrony
│   ├── ceph.yml
│   ├── openstack-init.yml
│   ├── markers.yml
│   └── node-remove.yml
└── terraform/             # libvirt VMs and networks from the node file
```

## Node file

`deploy/nodes/<env>.yml` is an Ansible YAML inventory and the single source of truth for the
cluster layout. Only a person edits it; scripts read it.

```yaml
all:
  vars:
    kolla_internal_vip_address: 10.10.0.10
    ceph_pool_size: 3
    network_interface: eth0              # management + tenant (Geneve)
    storage_interface: eth1              # Ceph public network
    neutron_external_interface: eth2     # no IP; provider network for floating IPs
    ceph_devices: [/dev/vdb]
  hosts:
    node1:
      ansible_host: 10.10.0.11
      storage_ip: 10.20.0.11
      node_roles: [control, network, compute, storage]
      vm: { vcpus: 4, memory_gb: 10, ceph_disk_gb: 50 }
    node2:
      ansible_host: 10.10.0.12
      storage_ip: 10.20.0.12
      node_roles: [compute, storage]
      vm: { vcpus: 4, memory_gb: 4, ceph_disk_gb: 50 }
    node3:
      ansible_host: 10.10.0.13
      storage_ip: 10.20.0.13
      node_roles: [compute, storage]
      vm: { vcpus: 4, memory_gb: 4, ceph_disk_gb: 50 }
```

- A node with a `vm:` block is created as a libvirt VM. A node without one must already exist
  (Ubuntu 24.04, reachable over SSH as a sudoer), so a production file is the same file without
  `vm:` blocks. The production file is written in the production step.
- `node_roles`, not `roles`, which is reserved in Ansible.
- node1 gets 10 GB, not 8: Kolla's all-in-one minimum is 8 GB before Ceph's mon, mgr, OSD and
  RGW. The default file uses 18 GB of the 24 GB WSL budget.

Every Ansible and Kolla command gets three inventories, in this order:
`-i deploy/nodes/<env>.yml -i deploy/nodes/roles.yml -i deploy/kolla/services.ini`.

- `roles.yml`: `plugin: ansible.builtin.constructed` with
  `keyed_groups: [{key: node_roles, prefix: role}]`, giving `role_control`, `role_network`,
  `role_compute`, `role_storage`.
- `services.ini`: the service section of Kolla's `multinode` example from the pinned release,
  with its top-level groups defined as children of ours:

| Role | Kolla groups | Ceph labels (cephadm) |
|---|---|---|
| `control` | control, monitoring, storage (cinder-volume) | `mon`, `mgr`, `rgw`, `_admin` |
| `network` | network (OVN gateway) | — |
| `compute` | compute | — |
| `storage` | — | `osd`, on the disks in `ceph_devices` |

Our `storage` means a Ceph disk host; Kolla's `storage` group means cinder-volume, which with
Ceph belongs on the controllers. The `role_` prefix keeps the two apart.

## Dev machines (Terraform)

- `terraform/` reads the node file with `yamldecode` and creates, for each host with `vm:`:
  a system volume from the Noble cloud image (40 GB), a Ceph volume (`ceph_disk_gb`, appears as
  `/dev/vdb`), and a domain with `host-passthrough` CPU and three NICs.
- Networks:

| libvirt network | Subnet | Mode | Use |
|---|---|---|---|
| `aleph0-mgmt` | 10.10.0.0/24 | NAT | management, API VIP, tenant overlay; internet for packages |
| `aleph0-storage` | 10.20.0.0/24 | isolated | Ceph public network |
| `aleph0-ext` | 10.30.0.0/24 | NAT | Neutron external network; floating IPs 10.30.0.100–199 |

- NIC MACs are derived from the node's last address octet, and cloud-init's network config
  matches each MAC and renames the NIC to `eth0`/`eth1`/`eth2`, so interface names are the same
  on every VM. cloud-init also sets the static IPs, the `ubuntu` user with
  `~/.ssh/id_ed25519.pub`, and the hostname.
- One Terraform workspace per env. Only `dev` has VMs.
- WSL reaches all three subnets through the libvirt bridges, so floating IPs answer ping from
  WSL.

## Rules

`playbooks/validate.yml` runs on `localhost` first and stops before anything changes:

- Odd number of controllers (1, 3, 5).
- At least one `network` node and one `compute` node.
- Storage hosts ≥ `ceph_pool_size`.
- Every node has `ansible_host`, `storage_ip` and known roles only.
- **Orphan check**, once a cluster exists (an admin `clouds.yaml` is present): any host that
  `ceph orch host ls` or `openstack compute service list` knows and the file does not stops the
  run: "nodeX is still part of the cluster: run `make node-remove ENV=<env> NODE=nodeX`, or put
  it back in the file."

The internal API address is the dedicated VIP with HAProxy + keepalived from day one
(`enable_haproxy: yes`), so endpoints and the API's `OS_AUTH_URL` do not move when controllers
join. Ceph replication is the explicit `ceph_pool_size`; nothing lowers it.

## `make infra-apply ENV=<env>`

`deploy/apply.sh <env>`, idempotent; the same command does the first bring-up, adds nodes and
adds roles:

1. **Validate** (above).
2. **Machines**: `terraform apply` in the env's workspace. Existing VMs are untouched. Then wait
   until every node answers SSH and cloud-init has finished.
3. **Pending**: `playbooks/pending.yml` reads `/etc/aleph0/roles` on every node. A node is
   pending when the marker is missing or lists different roles than the file. The list goes to
   `deploy/kolla/<env>/pending` (gitignored). When it is empty, steps 4–6 and 8 are skipped;
   step 7 still runs.
4. **Hosts**: `playbooks/hosts.yml` writes every node into `/etc/hosts` on **all** nodes and
   enables chrony (`customize_etc_hosts: no` in `globals.yml`: rerunning Kolla's
   `bootstrap-servers` on live nodes can restart Docker). Then
   `kolla-ansible bootstrap-servers --limit <pending>`.
5. **Ceph** (`playbooks/ceph.yml`):
   - first run: `cephadm bootstrap --mon-ip <first controller's storage_ip>` with
     `public_network 10.20.0.0/24`, dev `osd_memory_target` 1.5 GB;
   - every run: distribute the cephadm SSH key, `ceph orch host add` each control/storage node
     with its labels, apply the specs: `mon`, `mgr` by `label:mon` / `label:mgr`, `rgw default`
     by `label:rgw`, one OSD spec per storage host listing its `ceph_devices`;
   - pools `volumes`, `images`, `vms`, `backups` (created once, `size` = `ceph_pool_size` every
     run), users `client.glance`, `client.cinder`, `client.nova`, then `ceph.conf` + keyrings
     into `deploy/kolla/<env>/config/{glance,cinder/cinder-volume,cinder/cinder-backup,nova}`.
6. **Kolla**, with `--configdir deploy/kolla/<env>` and `--passwords` there:
   `kolla-genpwd` once, `prechecks` and `pull` on the pending nodes, `deploy` on the pending
   nodes, or on **all** nodes when a pending node involves `control`; `post-deploy` once, which
   writes the admin `clouds.yaml` into the config dir.
7. **OpenStack init** (`playbooks/openstack-init.yml`, `openstack.cloud` modules, idempotent):
   external network + subnet on `physnet1` (10.30.0.0/24, gateway .1, pool .100–.199), flavors
   `s.tiny` (1 vCPU, 256 MB, 1 GB), `s.small` (1, 1 GB, 10 GB), CirrOS image, project `dev` with
   a private network, subnet and router to the external network, user `aleph0-api` with
   `member` on `dev`, user `emergency-admin` with `admin`. Generated passwords go to
   `deploy/kolla/<env>/users.yml` (gitignored).
8. **Markers**: `playbooks/markers.yml` writes `/etc/aleph0/roles` on the pending nodes.

Kolla runs from `deploy/.venv`, a Make target built from `deploy/requirements.txt` (with
`kolla-ansible install-deps`), like `web/node_modules`.

## `make node-remove ENV=<env> NODE=<name>`

`deploy/node-remove.sh`. Only for nodes whose roles are all `compute` and/or `storage`; a node
with `control` or `network` is refused, and `deploy/README.md` documents the manual procedure.

1. Checks: the node is in the file; Ceph `HEALTH_OK`; at least one compute node remains;
   remaining storage hosts ≥ `ceph_pool_size`.
2. Compute: `openstack compute service set --disable`; if servers still run on the host, stop
   and print the live-migrate command (the node stays disabled); then
   `kolla-ansible stop --yes-i-really-really-mean-it --limit <node>`, delete the compute service
   and the node's network agents.
3. Storage: `ceph orch host drain <node>`, poll `ceph orch osd rm status` until empty,
   `ceph orch host rm <node>`.
4. Dev: `terraform destroy` of that node's domain and volumes only.
5. Print: "Remove <node> from deploy/nodes/<env>.yml." The script never edits the file.

If the file still lists the node, the next `infra-apply` adds it back as a new node, which is
what the file says.

## `make infra-destroy ENV=dev`

`terraform destroy` in the `dev` workspace, then delete `deploy/kolla/dev/{passwords.yml,
config/,pending,users.yml,clouds.yaml}`. Refuses any other env.

## Makefile

```make
infra-apply:   deploy/.venv/bin/kolla-ansible   # deploy/apply.sh $(ENV)
node-remove:   deploy/.venv/bin/kolla-ansible   # deploy/node-remove.sh $(ENV) $(NODE)
infra-destroy:                                  # dev only
infra-test:    deploy/.venv/bin/kolla-ansible   # validate fixtures + terraform validate
```

`ENV` defaults to `dev`. These targets run in WSL only (bash, libvirt).

## Host prerequisites (documented, installed once by hand)

`.wslconfig` with `memory=24GB`; nested virtualization (`/sys/module/kvm_*/parameters/nested`
= `Y`); packages `qemu-kvm libvirt-daemon-system genisoimage`; user in the `libvirt` and `kvm`
groups; Terraform; `uv`; an SSH key at `~/.ssh/id_ed25519`.

## Testing

`make infra-test`, no cluster needed:

- `ansible-playbook playbooks/validate.yml` against `nodes/testdata/valid.yml` passes, and
  against `two-controllers.yml`, `no-network.yml`, `too-few-storage.yml` fails. The orphan
  check is skipped because those fixtures have no `clouds.yaml`.
- `terraform validate`.

Everything else is covered by the acceptance criteria in #2, on real VMs.

## Docs

- `deploy/README.md` (layout above).
- `docs/architecture.md`: N nodes instead of a fixed 3 in the diagram, §3 dev environment
  (the default node file instead of the fixed table; roles), dev vs production table (1
  controller behind a VIP vs 3+ odd), §5 repo layout, §6 replaced by the make commands with
  `enable_haproxy: "yes"` and the VIP, §9 production notes for N nodes.

## Out of scope

Scripted removal of control and network nodes; dropping one role from a node; the production
node file and production secrets; Octavia; Ubuntu and Debian guest images.
