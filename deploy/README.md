# Infrastructure

OpenStack + Ceph on a set of nodes described by a node file. Everything here runs in WSL.

## Requirements (once)

- `C:\Users\<user>\.wslconfig`: `memory=24GB`; nested virtualization on
  (`cat /sys/module/kvm_intel/parameters/nested` or `kvm_amd` prints `Y`)
- Packages and groups:
  ```bash
  sudo apt-get install -y qemu-system-x86 qemu-utils libvirt-daemon-system python3-venv unzip
  sudo usermod -aG libvirt,kvm "$USER"    # then restart WSL
  ```
- Terraform 1.17.1 in `/usr/local/bin` (from releases.hashicorp.com)
- An SSH key at `~/.ssh/id_ed25519`

The Ansible/Kolla toolchain installs itself into `deploy/.venv` on first use.

## Commands

| Command | Does |
|---|---|
| `make infra-apply [ENV=dev]` | Brings the cluster to what `deploy/nodes/$(ENV).yml` says: first bring-up, new nodes, new roles |
| `make node-remove NODE=<name> [ENV=dev]` | Takes a compute and/or storage node out (drains Ceph, needs no servers left on it) |
| `make infra-stop` | Shuts the dev VMs down cleanly and frees most of their RAM (dev only) |
| `make infra-start` | Boots them again, waits for Ceph, and repairs the OpenStack services a boot leaves broken (dev only) |
| `make infra-reconfigure [TAGS=a,b]` | Applies `globals.yml` or Ceph key changes to the running cluster (`kolla-ansible reconfigure`) |
| `make infra-destroy` | Deletes the dev VMs and the generated files (dev only) |
| `make infra-test` | Checks the validation rules and the Terraform config, no cluster needed |

The first `infra-apply` takes about 30–60 minutes, mostly image pulls (32 minutes measured);
later runs only deploy the nodes that changed.

## Versions

- **Ceph 20.2.0** (Tentacle), matching the Ceph client libraries inside Kolla's 2026.2 images.
  20.2.4 monitors require the `aes256k` cephx cipher, which those clients cannot read. Upgrade
  Ceph together with Kolla's images. 20.2.0 lacks 20.2.4's cephx security fix.
- **Images:** dev uses Kolla's prebuilt quay.io test images (`kolla_test_images: "yes"` in
  `globals.yml`); production builds its own.

## Stopping and starting (dev)

Run `make infra-stop` when you are done for the day: it frees most of the VMs' RAM (one clean
run went from 19 GB used to 1 GB). Run it before `wsl --shutdown` or restarting Windows too:
VMs killed with WSL can leave MariaDB unable to start. It also tells Ceph not to re-replicate
data while every node is off.

`make infra-start` boots the VMs and waits up to 10 minutes for Ceph `HEALTH_OK`. If Ceph is not
healthy by then it warns and goes on; it stops only when Ceph has no quorum (rerun it).
Then it restarts the OpenStack containers that came up before MariaDB (clearing Neutron's stale
start-time file first) and checks every compute, network and volume service. It runs
`kolla-ansible mariadb-recovery` itself if the OpenStack API does not come back. It is safe on
a running cluster, and it is the way back after **any** WSL or Windows restart, even one
without `infra-stop`: a plain boot leaves some services broken. Servers inside OpenStack come
back `SHUTOFF`; start them with `openstack server start <name>`.

## The node file

`deploy/nodes/dev.yml` lists the nodes. Each node has `ansible_host`, `storage_ip` and
`node_roles`; a `vm:` block makes it a libvirt VM, without one it must be an existing Ubuntu
24.04 machine reachable over SSH (passwordless sudo, hostname equal to its name in the file).

| Role | Runs |
|---|---|
| `control` | OpenStack APIs, databases, RabbitMQ, OVN databases, cinder-volume; Ceph mon, mgr, RGW |
| `network` | OVN gateway (floating IPs, router traffic) |
| `compute` | nova-compute (tenant VMs) |
| `storage` | Ceph OSDs on the disks in `ceph_devices` |

Rules, checked before anything changes: 1, 3 or 5 controllers; at least one network and one
compute node; at least `ceph_pool_size` storage nodes.

Kolla variables never go in the node file: `deploy/kolla/<env>/globals.yml` maps the file's
own names (`vip_address`, `if_mgmt`, `if_storage`, `if_external`) onto Kolla's, so a node can
override them in its own entry.

### Dev sizing

- **node1 has 10 GB** (node1–3 use 18 GB of the 24 GB WSL budget). It fits because
  `globals.yml` sets `openstack_service_workers` and `openstack_service_rpc_workers` to 1
  (Kolla's default is one worker per vCPU), and VM nodes get a 2 GB swap file.
- `ceph_osd_memory_target` (1.5 GB, node file): caps each OSD's memory. Production uses Ceph's
  default of 4 GB; leave it unset there.
- `ceph_mon_max_pg_per_osd: 400` (node file): with one disk per node, the default pools exceed
  Ceph's 250 PGs per OSD and the cluster sits in `HEALTH_WARN`, which `node-remove` refuses to
  run in. Production, with more disks, leaves it unset.

## Adding a node

Add an entry to the node file and run `make infra-apply`. Only the new node is deployed, unless
it has the `control` role: then Kolla runs on every node (APIs may blip; tenant VMs keep
running). Controllers go from 1 to 3 in one apply.

## Removing a node

- Compute/storage: move the servers off it, run `make node-remove NODE=<name>`, then delete its
  entry from the node file. Deleting the entry first makes `infra-apply` refuse to run.
  `node-remove`:
  - plans its Terraform change first and refuses, before touching anything, unless it only
    deletes this node's VM (drift elsewhere: run `make infra-apply`, then retry);
  - is safe to rerun after an interruption;
  - runs `kolla-ansible destroy` on the node so a reboot cannot bring its containers back
    (skipped, with a message, when the node does not answer);
  - for a storage node, drops its Ceph OSD spec, drains it, waits until no Ceph daemons are
    left, removes the host, and removes its CRUSH bucket.
- Control or network (manual, for now; Kolla's "Removing existing controllers" order). Keep
  an odd number of controllers, and remove one at a time. From the repo root, define `ka`, which
  runs Kolla the way `deploy/infra.sh` does (Kolla calls `ansible-playbook`, so the venv must be
  on the `PATH`; dev VMs are recreated with new host keys, so host key checking is off):
  ```bash
  export PATH=$PWD/deploy/.venv/bin:$PATH
  export ANSIBLE_HOST_KEY_CHECKING=False     # dev only
  ka() { kolla-ansible "$1" -i deploy/nodes/dev.yml -i deploy/nodes/roles.yml -i deploy/kolla/services.ini --configdir "$PWD/deploy/kolla/dev" --passwords "$PWD/deploy/kolla/dev/passwords.yml" "${@:2}"; }
  export OS_CLIENT_CONFIG_FILE=$PWD/deploy/kolla/dev/clouds.yaml OS_CLOUD=kolla-admin
  ```
  1. Stop its services: `ka stop --yes-i-really-really-mean-it --limit <node>`.
  2. Delete its entry from the node file.
  3. Controller: `ka deploy --limit control`, so MariaDB and RabbitMQ drop the old member.
     Network: OVN moves the routers' gateways to the remaining network nodes.
  4. Check the cluster: `openstack compute service list`, `openstack network agent list`.
  5. Delete its network agents and compute service, as in `deploy/playbooks/node-remove.yml`.
  6. If it is a Ceph host, on the first controller (`ssh ubuntu@<its ansible_host>`), each as
     `sudo cephadm shell -- ceph ...`:
     1. `orch rm osd.<node> --force`: drain does nothing while an OSD spec names the host;
     2. `orch host drain <node>`;
     3. wait until `orch ps <node>` lists no daemons;
     4. `orch host rm <node>`, then `osd crush remove <node>`.
  7. Delete `/etc/aleph0/roles` on the node.

## Troubleshooting

- **`connection reset by peer` while pulling images:** quay.io (served from an ISP-local
  cache) drops long downloads. `infra-apply` retries the Kolla pull 3 times; if it still fails,
  run it again: everything is safe to rerun and finished layers are kept.
- **Changed `globals.yml`, or Ceph was rebuilt:** `infra-apply` only runs Kolla for pending
  nodes, so apply the change with `make infra-reconfigure` (`TAGS=glance,cinder,nova` after a
  Ceph rebuild, which issues new keys).
- **`post-deploy` died halfway:** delete `deploy/kolla/<env>/clouds.yaml` and rerun
  `make infra-apply`, which runs `post-deploy` again while the file is missing.
- **Lost `deploy/kolla/<env>/credentials/aleph0-api`:** the next apply writes a new password
  file, but Keystone keeps the old password (`update_password: on_create`). With `OS_CLOUD`
  and `OS_CLIENT_CONFIG_FILE` set as in [Reaching OpenStack from WSL](#reaching-openstack-from-wsl),
  reset it with
  `openstack user set --password "$(cat deploy/kolla/dev/credentials/aleph0-api)" aleph0-api`.
- **An old `/usr/local/bin/cephadm` regular file** (from before the versioned binaries) makes
  the link task fail: delete it and rerun.
- **A removed physical node that was unreachable** keeps its Kolla containers, and may rejoin
  after a reboot. `node-remove` says so and finishes the rest. Keep the node in the node file
  (and do not run `infra-apply`, which would add it back) until it answers and you have run
  `ka destroy --yes-i-really-really-mean-it --limit <node>` (the `ka` function and the
  `export`s from [Removing a node](#removing-a-node)); then delete the entry.
- **VMs were forced off, or WSL died without `infra-stop`:** run `make infra-start`. It runs
  `mariadb-recovery` itself when the OpenStack API does not come back, and it clears Neutron's
  stale `/tmp/neutron_start_time*` file, which otherwise stops `neutron_server` from registering
  again after a restart.
- **`infra-start` says "Ceph is not up yet":** the monitors were not in quorum within 10
  minutes. Rerun it. (Ceph up but not `HEALTH_OK` after 10 minutes is only a warning.)

## Reaching OpenStack from WSL

```bash
export OS_CLIENT_CONFIG_FILE=$PWD/deploy/kolla/dev/clouds.yaml OS_CLOUD=kolla-admin
deploy/.venv/bin/openstack server list --all-projects
```

The APIs are on the VIP `10.10.0.10`. The backend's user is `aleph0-api` (`member` on project
`dev`), password in `deploy/kolla/dev/credentials/aleph0-api`. Kolla's `admin` is the local
emergency admin (not federated), password `keystone_admin_password` in
`deploy/kolla/dev/passwords.yml`. Floating IPs (10.30.0.100–199) answer from WSL.

## Generated files

`deploy/kolla/<env>/` holds, all gitignored (only `globals.yml` is committed):

- `passwords.yml`, `clouds.yaml`, `credentials/`, `pending.env`
- the Ceph keys and `ceph.conf` in `config/`
- `admin-openrc*.sh` and `public-openrc*.sh`, written by Kolla's `post-deploy`
- `cirros-*.img`, the downloaded CirrOS image
- `remove-<node>.tfplan`, only while `node-remove` runs

Each node records its deployed roles in `/etc/aleph0/roles`. Terraform's state (one workspace
per env) lives in `deploy/terraform/terraform.tfstate.d/`, outside `deploy/kolla/`.
