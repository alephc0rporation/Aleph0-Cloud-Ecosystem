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
| Ansible | `ansible-core` 2.20–2.21 (Kolla's pin), host `python3` (3.14) | that range supports Python 3.12–3.14 on the controller |
| Ceph | Tentacle 20.2.4: image `quay.io/ceph/ceph:v20.2.4`, cephadm from `download.ceph.com/rpm-20.2.4` | latest stable; Noble's archive still ships Squid's cephadm |
| Terraform | 1.17.1, provider `dmacvicar/libvirt` 0.9.9 (`.terraform.lock.hcl` committed) | already chosen in `docs/architecture.md` |
| `openstack.cloud` collection | 2.6.0 | the OpenStack init playbook |

The deploy machine is WSL (`Ubuntu-26.04`), where `make` runs. Kolla's `deployment` group is
`localhost`. Kolla's images carry their own Ceph client libraries; Ceph clients talk to newer
clusters, so a Tentacle cluster serves them.

## Layout

```
deploy/
├── README.md              # requirements, commands, adding/removing nodes, manual controller removal
├── requirements.txt       # ansible-core, kolla-ansible (git, stable/2026.2), openstacksdk, openstackclient
├── requirements.yml       # Ansible collections: openstack.cloud
├── infra.sh               # apply | remove | destroy | test <env> [node]
├── nodes/
│   ├── dev.yml            # the dev cluster
│   ├── roles.yml          # constructed plugin: node_roles → role_* groups
│   └── testdata/          # fixture node files for infra-test
├── kolla/
│   ├── services.ini       # Kolla's service → group mapping, pointed at role_* groups
│   └── dev/
│       ├── globals.yml    # the only committed file in this folder
│       ├── passwords.yml  # generated
│       ├── clouds.yaml    # written by post-deploy (cloud `kolla-admin`)
│       ├── pending.env    # written by apply
│       ├── credentials/   # generated OpenStack user passwords
│       └── config/        # ceph.conf + keyrings for glance/cinder/nova, glance.conf
├── playbooks/
│   ├── group_vars/all.yml # Ceph version and image
│   ├── templates/         # cephadm specs, pool script
│   ├── validate.yml       # rules + orphan check
│   ├── hosts.yml          # wait for nodes, /etc/hosts on all nodes
│   ├── pending.yml        # reads markers, writes pending.env
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
    ansible_user: ubuntu
    vip_address: 10.10.0.10            # internal API VIP
    if_mgmt: eth0                      # management, API, tenant overlay (Geneve)
    if_storage: eth1                   # Ceph public network
    if_external: eth2                  # no IP; Neutron provider network for floating IPs
    ceph_pool_size: 3
    ceph_devices: [/dev/vdb]
    ceph_osd_memory_target: 1610612736 # dev only: 1.5 GB per OSD
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
  (Ubuntu 24.04, reachable over SSH as a passwordless sudoer, hostname equal to its name in the
  file), so a production file is the same file without `vm:` blocks. The production file is
  written in the production step.
- `node_roles`, not `roles`, which is reserved in Ansible.
- The file uses its own variable names (`vip_address`, `if_*`), never Kolla's: Kolla's
  playbook `group_vars/all` outrank inventory group vars, so a Kolla variable set under
  `all.vars` would be silently ignored. `globals.yml` maps ours onto Kolla's
  (`network_interface: "{{ if_mgmt }}"`, …), and any of them can be overridden per host.
- node1 gets 10 GB, not 8: Kolla's all-in-one minimum is 8 GB before Ceph's mon, mgr, OSD and
  RGW. The default file uses 18 GB of the 24 GB WSL budget.

Every Ansible and Kolla command gets three inventories, in this order:
`-i deploy/nodes/<env>.yml -i deploy/nodes/roles.yml -i deploy/kolla/services.ini`.

- `roles.yml`: `plugin: ansible.builtin.constructed` with
  `keyed_groups: [{key: node_roles, prefix: role}]`, giving `role_control`, `role_network`,
  `role_compute`, `role_storage`.
- `services.ini`: a short header that defines Kolla's top-level groups as children of ours,
  followed by everything from `[baremetal:children]` on in Kolla's `multinode` example from the
  pinned release. The header also puts `role_storage` under `baremetal`, so
  `bootstrap-servers` installs Docker on storage-only nodes for cephadm.

| Role | Kolla groups | Ceph labels (cephadm) |
|---|---|---|
| `control` | control, monitoring, storage (cinder-volume) | `mon`, `mgr`, `rgw`, `_admin` |
| `network` | network (OVN gateway) | — |
| `compute` | compute | — |
| `storage` | baremetal only (Docker) | `osd`, on the disks in `ceph_devices` |

Our `storage` means a Ceph disk host; Kolla's `storage` group means cinder-volume, which with
Ceph belongs on the controllers. The `role_` prefix keeps the two apart.

## Dev machines (Terraform)

- `terraform/` takes the node file as `-var nodes_file=…`, reads it with `yamldecode`, and
  creates, for each host with `vm:`: a 40 GB system volume backed by the Noble cloud image, a
  Ceph volume (`ceph_disk_gb`, appears as `/dev/vdb`), a cloud-init disk, and a domain with
  `host-passthrough` CPU and three NICs, all in a `dir` pool at
  `/var/lib/libvirt/images/aleph0`.
- Networks:

| libvirt network | Subnet | Mode | Use |
|---|---|---|---|
| `aleph0-mgmt` | 10.10.0.0/24 | NAT | management, API VIP, tenant overlay; internet for packages |
| `aleph0-storage` | 10.20.0.0/24 | isolated | Ceph public network |
| `aleph0-ext` | 10.30.0.0/24 | NAT | Neutron external network; floating IPs 10.30.0.100–199 |

- NIC MACs are `52:54:00:<net>:00:<last octet of ansible_host>` (`<net>` = 10, 20, 30), and
  cloud-init's network config matches each MAC and renames the NIC to `eth0`/`eth1`/`eth2`, so
  interface names are the same on every VM. cloud-init also sets the static IPs, the
  `ansible_user` with `~/.ssh/id_ed25519.pub` and passwordless sudo, and the hostname.
- One Terraform workspace per env. `infra.sh` skips Terraform when the node file has no `vm:`.
- WSL reaches all three subnets through the libvirt bridges, so floating IPs answer ping from
  WSL.

## Rules

`playbooks/validate.yml` runs on `localhost` first and stops before anything changes:

- Every node has `ansible_host`, `storage_ip` and at least one role, all from
  `control`, `network`, `compute`, `storage`.
- Odd number of controllers (1, 3, 5).
- At least one `network` node and one `compute` node.
- Storage hosts ≥ `ceph_pool_size`.
- **Orphan check**, once a cluster exists (`deploy/kolla/<env>/clouds.yaml` is present): any
  host that `ceph orch host ls` or `openstack network agent list` knows and the file does not
  stops the run (every compute and network node runs an OVN agent; every controller is a Ceph
  host): "nodeX: still part of the cluster: run `make node-remove ENV=<env>
  NODE=nodeX`, or put it back in the file."

The internal API address is the dedicated VIP with HAProxy + keepalived from day one
(`enable_haproxy: yes`), so endpoints and the API's `OS_AUTH_URL` do not move when controllers
join. Ceph replication is the explicit `ceph_pool_size`; nothing lowers it.

## `make infra-apply ENV=<env>`

`deploy/infra.sh apply <env>`, idempotent; the same command does the first bring-up, adds nodes
and adds roles:

1. **Validate** (above).
2. **Machines**: `terraform apply` in the env's workspace. Existing VMs are untouched.
3. **Hosts** (`playbooks/hosts.yml`): wait for SSH and, on VMs, for cloud-init; drop the
   `127.0.1.1 <hostname>` line; write every node into `/etc/hosts` on **all** nodes
   (`customize_etc_hosts: no` in `globals.yml`: rerunning Kolla's `bootstrap-servers` on live
   nodes can restart Docker).
4. **Pending** (`playbooks/pending.yml`): a node is pending when its `/etc/aleph0/roles` marker
   is missing or lists different roles than the file. Writes `deploy/kolla/<env>/pending.env`
   with `PENDING=<comma-separated nodes>` and `PENDING_CONTROL=yes|no`. When `PENDING` is
   empty, steps 5, 7 and 9 are skipped; steps 6 and 8 still run, so changes to
   `ceph_pool_size` and the Ceph specs apply without a node change.
5. **Docker**: `kolla-ansible bootstrap-servers --limit <pending>`; when `PENDING_CONTROL=yes`
   the limit also includes `role_control`, as Kolla requires when controllers are added.
6. **Ceph** (`playbooks/ceph.yml`, every run):
   - install cephadm on the controllers; first run: `cephadm bootstrap --mon-ip <first
     controller's storage_ip> --ssh-user <ansible_user> --skip-monitoring-stack
     --skip-dashboard`;
   - authorize the cephadm SSH key on every control/storage node;
   - apply one spec file: a `host` spec per control/storage node (address = `storage_ip`,
     labels from its roles), `mon` and `mgr` by `label:mon` / `label:mgr`, `rgw.default` by
     `label:rgw` on port 8080, one `osd` spec per storage host listing its `ceph_devices`;
   - pool script: `osd_memory_target` when set, pools `volumes`, `images`, `vms`, `backups`
     (`size` = `ceph_pool_size`, `min_size` = size − size/2, every run), users `client.glance`,
     `client.cinder`, `client.cinder-backup` (Nova uses `client.cinder`, Kolla's default);
   - wait until every disk in `ceph_devices` is an up OSD;
   - fetch `ceph.conf` + keyrings into `deploy/kolla/<env>/config/` in Kolla's external-Ceph
     layout, plus `glance.conf` with `show_image_direct_url = True`.
7. **Kolla**, with `--configdir deploy/kolla/<env>` and `--passwords` there: `kolla-genpwd`
   once, `prechecks` and `pull` with the step-5 limit, `deploy` with that limit, or on **all**
   nodes when `PENDING_CONTROL=yes`; `post-deploy` while `clouds.yaml` is missing.
8. **OpenStack init** (`playbooks/openstack-init.yml`, `openstack.cloud` modules, idempotent):
   external network `public` (flat, `physnet1`) with subnet 10.30.0.0/24, gateway .1, pool
   .100–.199, no DHCP; flavors `s.tiny` (1 vCPU, 256 MB, 1 GB) and `s.small` (1, 1 GB, 10 GB);
   public image `cirros` (0.6.3); project `dev` with network `dev-net`, subnet
   192.168.100.0/24 and router `dev-router` to `public`; ICMP and SSH allowed in `dev`'s
   default security group; user `aleph0-api` with `member` on `dev`, password generated into
   `deploy/kolla/<env>/credentials/`. Kolla's own `admin` (local, not federated, password in
   `passwords.yml`) is the emergency admin; no second admin account.
9. **Markers** (`playbooks/markers.yml`): write `/etc/aleph0/roles` on the pending nodes.

Kolla runs from `deploy/.venv`, a Make target built from `deploy/requirements.txt` and
`deploy/requirements.yml` (plus `kolla-ansible install-deps`), like `web/node_modules`. In dev,
`infra.sh` turns off SSH host key checking, because VMs are recreated with the same addresses.

## `make node-remove ENV=<env> NODE=<name>`

`deploy/infra.sh remove <env> <node>`. Only for nodes whose roles are all `compute` and/or
`storage`; a node with `control` or `network` is refused, and `deploy/README.md` documents the
manual procedure.

1. Checks (`node-remove.yml`, tag `check`): the node is in the file; its roles are allowed; at
   least one other compute node remains; remaining storage hosts ≥ `ceph_pool_size`; Ceph
   `HEALTH_OK`. Compute: `openstack compute service set --disable <node> nova-compute`; if
   servers still run there, stop and print the live-migrate command (the node stays disabled).
2. `kolla-ansible stop --yes-i-really-really-mean-it --ignore-missing --limit <node>`.
3. Cleanup (tag `cleanup`): compute: delete the node's network agents and compute service
   (`--os-compute-api-version 2.53`). Storage: `ceph orch host drain <node>`, wait until
   `ceph orch ps <node>` lists no daemons, `ceph orch host rm <node>`. Delete the node's marker
   (ignored if the node is unreachable).
4. Dev: `terraform destroy` targeting that node's domain and volumes only.
5. Print: "Remove <node> from deploy/nodes/<env>.yml." The script never edits the file.

If the file still lists the node, the next `infra-apply` adds it back as a new node, which is
what the file says.

## `make infra-destroy ENV=dev`

`terraform destroy` in the `dev` workspace, then delete everything in `deploy/kolla/dev/` except
`globals.yml`. Refuses any other env.

## Makefile

```make
ENV ?= dev
infra-apply:   deploy/.venv/.done   # deploy/infra.sh apply $(ENV)
node-remove:   deploy/.venv/.done   # deploy/infra.sh remove $(ENV) $(NODE)
infra-destroy:                      # deploy/infra.sh destroy $(ENV)
infra-test:    deploy/.venv/.done   # deploy/infra.sh test $(ENV)
```

`deploy/.venv/.done` is written after the last install step, so a failed install runs again.
These targets run in WSL only (bash, libvirt).

## Host prerequisites (documented, installed once by hand)

`.wslconfig` with `memory=24GB`; nested virtualization (`/sys/module/kvm_*/parameters/nested`
= `Y`); packages `qemu-system-x86 qemu-utils libvirt-daemon-system python3-venv unzip`; the user in the `libvirt`
and `kvm` groups; Terraform 1.17.1 in `/usr/local/bin`; an SSH key at `~/.ssh/id_ed25519`.

## Testing

`make infra-test`, no cluster needed:

- `ansible-playbook playbooks/validate.yml` against `nodes/testdata/valid.yml` passes, and
  against `two-controllers.yml`, `no-network.yml`, `too-few-storage.yml`, `unknown-role.yml`, `empty.yml`
  fails. The orphan check is skipped because no `clouds.yaml` is passed.
- `terraform validate`.

Everything else is covered by the acceptance criteria in #2, on real VMs.

## Docs

- `deploy/README.md` (layout above).
- `docs/architecture.md`: N nodes instead of a fixed 3 in the diagram, §3 dev environment
  (the default node file instead of the fixed table; roles), dev vs production table (1
  controller behind a VIP vs 3+ odd), §4 tools, §5 repo layout, §6 replaced by the make
  commands with `enable_haproxy: "yes"` and the VIP, §9 production notes for N nodes.

## Out of scope

Scripted removal of control and network nodes; dropping one role from a node; the production
node file and production secrets; Octavia; Kolla's RGW integration (Keystone endpoint, HAProxy
frontend: `enable_ceph_rgw: no` until the S3 step; Ceph runs RGW itself); Ubuntu and Debian
guest images.
