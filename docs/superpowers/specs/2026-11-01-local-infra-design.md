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
| Ceph | Tentacle 20.2.0: image `quay.io/ceph/ceph:v20.2.0`, cephadm from `download.ceph.com/rpm-20.2.0` | matches the Ceph client libraries in Kolla's 2026.2 images; 20.2.4 monitors require the `aes256k` cephx cipher those clients cannot read. Upgrade Ceph together with Kolla's images; 20.2.0 lacks 20.2.4's cephx security fix |
| Terraform | 1.17.1, provider `dmacvicar/libvirt` 0.9.9 (`.terraform.lock.hcl` committed) | already chosen in `docs/architecture.md` |
| `openstack.cloud` collection | 2.6.0 | the OpenStack init playbook |

The deploy machine is WSL (`Ubuntu-26.04`), where `make` runs. Kolla's `deployment` group is
`localhost`. Kolla's images carry their own Ceph client libraries (20.2.0), so the Ceph cluster is
pinned to that release. Dev uses Kolla's quay.io test images (`kolla_test_images: "yes"` in the dev
`globals.yml`); production builds its own images.

## Layout

```
deploy/
├── README.md              # requirements, commands, adding/removing nodes, manual controller removal
├── requirements.txt       # ansible-core, kolla-ansible (git, stable/2026.2), openstacksdk, openstackclient
├── requirements.yml       # Ansible collections: openstack.cloud
├── infra.sh               # apply | remove | destroy | stop | start | reconfigure | test <env> [node|tags]
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
│   ├── node-remove.yml
│   └── start.yml          # brings OpenStack back after the dev VMs boot
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
    ceph_mon_max_pg_per_osd: 400       # dev only: one OSD per node
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
  RGW. The default file uses 18 GB of the 24 GB WSL budget. Dev runs one worker per OpenStack
  service and a 2 GB swap file on VM nodes, so node1 fits in 10 GB.

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
| `network` | network (OVN gateway), loadbalancer (HAProxy + keepalived, the API VIP) | — |
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
  NODE=nodeX`, or put it back in the file." When the first controller has no `cephadm` yet
  (before Ceph's first run, or while Ceph is rebuilt), it skips Ceph's hosts and says so.
- **Ceph bootstrap host first**: Ceph is administered from the first controller in the file,
  and `ceph.yml` bootstraps Ceph there when it has none, so a controller listed above the
  bootstrap host would start a second Ceph cluster. The controller with `/etc/ceph/ceph.pub`
  (only the bootstrap host has it; controllers that do not answer yet are skipped) must be
  first: "Ceph runs from node1: keep it first among the controllers in the node file". Chosen
  over finding the admin host dynamically, which would touch every place that runs Ceph
  commands (`ceph.yml`, `validate.yml`, `node-remove.yml`, `infra.sh` stop/start) for an edit
  that a one-line rule avoids.

The internal API address is the dedicated VIP with HAProxy + keepalived from day one
(`enable_haproxy: yes`), so endpoints and the API's `OS_AUTH_URL` do not move when controllers
join. Ceph replication is the explicit `ceph_pool_size`; nothing lowers it.

## `make infra-apply ENV=<env>`

`deploy/infra.sh apply <env>`, idempotent; the same command does the first bring-up, adds nodes
and adds roles. In dev, when any `aleph0-*` VM is defined but not running, it stops at once
with "dev VMs are stopped: run make infra-start first" (so do `node-remove` and
`infra-reconfigure`).

1. **Validate** (above).
2. **Machines**: Terraform in the env's workspace. Before a cluster exists (no `clouds.yaml`),
   `terraform apply`. Once it exists, `terraform plan` to `deploy/kolla/<env>/apply.tfplan`;
   if any resource change contains `delete` (a replace included: a new `ansible_host` or
   `storage_ip` replaces the cloud-init disk, and so do the SSH key and the Noble image URL),
   it stops before applying anything, lists the resources and says to undo the change or
   remove the node and add it back. Otherwise it applies the saved plan: new nodes and in-place
   updates (provider 0.9.9 updates `vcpus`, `memory_gb` and `ceph_disk_gb`, a volume resize,
   in place). The check is the same helper `node-remove` uses, with another rule.
3. **Hosts** (`playbooks/hosts.yml`): wait for SSH and, on VMs, for cloud-init (exit code 2,
   degraded but usable, is accepted); drop the `127.0.1.1 <hostname>` line; write every node
   into `/etc/hosts` on **all** nodes (`customize_etc_hosts: no` in `globals.yml`: rerunning
   Kolla's `bootstrap-servers` on live nodes can restart Docker). On VMs, a 2 GB `/swapfile`,
   formatted under a temporary name and then moved into place, so a cut run never leaves an
   unformatted one.
4. **Pending** (`playbooks/pending.yml`): a node is pending when its `/etc/aleph0/roles` marker
   is missing or lists different roles than the file. A marker role that the file dropped
   stops the run before `pending.env` is written: dropping a role is not supported (its
   services would keep running; a storage node would keep its OSD and `node-remove` would skip
   its drain), so the node is removed and added back. Writes `deploy/kolla/<env>/pending.env`
   with `PENDING=<comma-separated nodes>` and `PENDING_CONTROL=yes|no`. When `PENDING` is
   empty, steps 5 and 7 are skipped; steps 6 and 8 still run, so changes to
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
   - wait until every disk in `ceph_devices` is an up OSD (a failing `cephadm` call is
     retried);
   - fetch `ceph.conf` + keyrings into `deploy/kolla/<env>/config/` in Kolla's external-Ceph
     layout, plus `glance.conf` with `show_image_direct_url = True`.
7. **Kolla**, with `--configdir deploy/kolla/<env>` and `--passwords` there: `kolla-genpwd`
   once, `prechecks` and `pull` with the step-5 limit (`pull` up to 3 attempts 30 s apart:
   quay.io resets connections mid-download), `deploy` with that limit, or on **all**
   nodes when `PENDING_CONTROL=yes`; `post-deploy` while `clouds.yaml` is missing. Then
   **markers** (`playbooks/markers.yml`): write `/etc/aleph0/roles` on the pending nodes,
   before the init, so a failed init does not make the next apply redeploy them.
8. **OpenStack init** (`playbooks/openstack-init.yml`, `openstack.cloud` modules, idempotent):
   external network `public` (flat, `physnet1`) with subnet 10.30.0.0/24, gateway .1, pool
   .100–.199, no DHCP; flavors `s.tiny` (1 vCPU, 256 MB, 1 GB) and `s.small` (1, 1 GB, 10 GB);
   public image `cirros` (0.6.3, downloaded with its SHA-256, so an existing file is not
   fetched again, and retried); project `dev` with network `dev-net`, subnet
   192.168.100.0/24 and router `dev-router` to `public`; ICMP and SSH allowed in `dev`'s
   default security group; user `aleph0-api` with `member` on `dev`, password generated into
   `deploy/kolla/<env>/credentials/`. Kolla's own `admin` (local, not federated, password in
   `passwords.yml`) is the emergency admin; no second admin account.

Kolla runs from `deploy/.venv`, a Make target built from `deploy/requirements.txt` and
`deploy/requirements.yml` (plus `kolla-ansible install-deps`), like `web/node_modules`. In dev,
`infra.sh` turns off SSH host key checking, because VMs are recreated with the same addresses.

## `make node-remove ENV=<env> NODE=<name>`

`deploy/infra.sh remove <env> <node>`. Only for nodes whose roles are all `compute` and/or
`storage`; a node with `control` or `network` is refused, and `deploy/README.md` documents the
manual procedure. Every step is safe to rerun after an interruption.

1. Dev (the file has `vm:` blocks), before anything touches the node: `terraform plan` of the
   node file without the node, to `deploy/kolla/<env>/remove-<node>.tfplan`. It refuses unless
   every change is a `delete` of that node's resources ("Terraform would change more than
   <node>; run make infra-apply first and retry"). A targeted `terraform destroy` is not used:
   every domain depends on the shared volume resources, so it would take every VM with it.
2. Checks (`node-remove.yml`, tag `check`): the node is in the file; its roles are allowed; at
   least one other compute node remains; remaining storage hosts ≥ `ceph_pool_size`; Ceph
   `HEALTH_OK` (any `HEALTH_WARN` refuses) while the node is still a Ceph host, so a rerun
   after it left Ceph does not depend on the cluster's health. Compute: if its `nova-compute`
   service still exists, `openstack compute service set --disable <node> nova-compute`; if
   servers still run there, stop and print the live-migrate command and the
   `openstack compute service set --enable <node> nova-compute` that undoes the disable (the
   node stays disabled).
3. When the node answers: `kolla-ansible destroy --yes-i-really-really-mean-it --limit <node>`
   (not `stop`: a physical node that reboots must not bring its containers back). When it
   does not, skip it and say so: a physical node stays in the file until `destroy` has run on
   it (`deploy/README.md`).
4. Cleanup (tag `cleanup`): compute: delete the node's network agents and compute service
   (`--os-compute-api-version 2.53`). Storage, while the node is still a Ceph host:
   `ceph orch rm osd.<node> --force` (drain refuses a host an OSD spec names, with exit code 0;
   the OSDs keep running), `ceph orch host drain <node>`, wait until `ceph orch ps <node>`
   lists no daemons (a failing `cephadm` call is retried), `ceph orch host rm <node>`; then,
   always, `ceph osd crush remove <node>` (`host rm` leaves the empty bucket; a missing one is
   not an error). Delete the node's marker (ignored if the node is unreachable).
5. Dev: `terraform apply` of the saved plan, which deletes only the node's VM and volumes.
6. Print: "Remove <node> from deploy/nodes/<env>.yml." (for a skipped physical node: keep it
   in the file until its Kolla cleanup has run). The script never edits the file.

If the file still lists the node, the next `infra-apply` adds it back as a new node, which is
what the file says.

## `make infra-destroy ENV=dev`

Asks "Destroy the dev cluster? [y/N]" when stdin is a terminal (scripts and CI are not asked),
then `terraform destroy` in the `dev` workspace and deletes everything in `deploy/kolla/dev/`
except `globals.yml`. Refuses any other env.

## `make infra-stop ENV=dev`

`deploy/infra.sh stop dev` frees the dev VMs' RAM. Refuses any other env. Stops with an error
when `virsh` cannot reach libvirt; with no `aleph0-*` VM running it only says they are off.

1. `ceph osd set noout`, so Ceph does not re-replicate while every node is down (if Ceph does
   not answer, it says so and goes on).
2. `virsh shutdown` of every running `aleph0-*` domain; wait up to 5 minutes, then
   `virsh destroy` the ones still running, check they are off, and say that `make infra-start`
   runs `mariadb-recovery` itself if the OpenStack API does not come back.

## `make infra-start ENV=dev`

`deploy/infra.sh start dev`. Refuses any other env. It is also the way back after any host
restart (WSL or Windows): a plain boot leaves OpenStack services broken, and this repairs them.
It is safe to run on a running cluster.

1. `virsh start` every stopped `aleph0-*` domain; `playbooks/hosts.yml` waits for SSH.
2. Within 10 minutes: `ceph osd unset noout`, retried until the monitors answer (every `ceph`
   call has a 30 s connect timeout), then wait for `HEALTH_OK`. If noout could not be unset:
   "Ceph is not up yet; rerun make infra-start", exit 1. If Ceph is not `HEALTH_OK` by then,
   print a warning with its health line and go on.
3. With `clouds.yaml` present, `playbooks/start.yml`:
   - on the controllers, wait until `mariadb`, `proxysql` and `rabbitmq` are healthy;
   - on every node, restart the Kolla containers (label `kolla_version`; cephadm's are never
     touched) that are `unhealthy` or `exited`, after their health checks settle, in up to
     3 rounds. API containers that started before MariaDB answered stay unhealthy, because
     uwsgi does not retry. Before `neutron_server` is restarted, its `/tmp/neutron_start_time*`
     is deleted: Neutron keys the WSGI start time on the uwsgi PID and `/tmp` survives a
     restart, so when the PIDs repeat (often, in a container) the old time comes back, the
     previous run's `ovn_hash_ring` row stays, and registering again fails with a duplicate key;
   - wait until every compute service, network agent and volume service is up (each list also
     needs a token).
4. If `start.yml` fails and `openstack token issue` does not answer for 5 minutes:
   `kolla-ansible mariadb-recovery`, then `start.yml` again. If the API does answer, MariaDB is
   fine: it stops and asks to fix the failed task and rerun `make infra-start`.
5. Print: servers inside OpenStack come back `SHUTOFF`; `openstack server start <name>`.

## `make infra-reconfigure ENV=<env> [TAGS=a,b]`

`deploy/infra.sh reconfigure <env> [tags]`: `kolla-ansible reconfigure`, with `--tags` when
given. `infra-apply` runs Kolla only for pending nodes, so changes to `globals.yml` or rebuilt
Ceph keys reach a running cluster this way.

## Makefile

```make
ENV ?= dev
infra-apply:   deploy/.venv/.done   # deploy/infra.sh apply $(ENV)
node-remove:   deploy/.venv/.done   # deploy/infra.sh remove $(ENV) $(NODE)
infra-destroy:                      # deploy/infra.sh destroy $(ENV)
infra-test:    deploy/.venv/.done   # deploy/infra.sh test $(ENV)
infra-stop:    deploy/.venv/.done   # deploy/infra.sh stop $(ENV)
infra-start:   deploy/.venv/.done   # deploy/infra.sh start $(ENV)
infra-reconfigure: deploy/.venv/.done  # deploy/infra.sh reconfigure $(ENV) $(TAGS)
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
  fails. The checks against an existing cluster are skipped because no `cluster_dir` is passed.
- `fresh-cluster.yml` passes with `cluster_dir` set to a folder without `clouds.yaml` while its
  controller does not answer (`ANSIBLE_TIMEOUT=5`): the state of a first `infra-apply`, so the
  Ceph bootstrap host check runs with no controller having Ceph.
- The Terraform plan check on canned `terraform show -json` plans, one allowed and one refused
  for each rule: for `infra-apply` a create passes and a replace is refused; for removing node3
  deleting node3's volume passes and deleting it plus creating node4 is refused.
- `terraform validate`.

Everything else is covered by the acceptance criteria in #2, on real VMs.

## Docs

- `deploy/README.md` (layout above).
- `docs/architecture.md`: N nodes instead of a fixed 3 in the diagram, §3 dev environment
  (the default node file instead of the fixed table; roles), dev vs production table (1
  controller behind a VIP vs 3+ odd), §4 tools, §5 repo layout, §6 replaced by the make
  commands with `enable_haproxy: "yes"` and the VIP, §9 production notes for N nodes.

## Out of scope

Scripted removal of control and network nodes; dropping one role from a node (`infra-apply`
refuses it); the production
node file and production secrets; Octavia; Kolla's RGW integration (Keystone endpoint, HAProxy
frontend: `enable_ceph_rgw: no` until the S3 step; Ceph runs RGW itself); Ubuntu and Debian
guest images.
