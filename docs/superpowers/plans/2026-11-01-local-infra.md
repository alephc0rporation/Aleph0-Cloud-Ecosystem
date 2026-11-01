# Local infra Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `make infra-apply` builds and grows an OpenStack + Ceph cluster from a node file, `make node-remove` takes compute/storage nodes out, `make infra-destroy` wipes the dev cluster (issue #2).

**Architecture:** `deploy/nodes/<env>.yml` is an Ansible inventory listing nodes with `node_roles`. Terraform turns nodes with a `vm:` block into libvirt VMs; our playbooks prepare hosts and Ceph (cephadm); Kolla-Ansible deploys OpenStack with the role groups mapped onto Kolla's groups. `deploy/infra.sh` runs the steps; a marker on each node (`/etc/aleph0/roles`) tells which nodes still need deploying.

**Tech Stack:** Bash, Ansible (`ansible-core` 2.20–2.21), Kolla-Ansible `stable/2026.2`, cephadm / Ceph 20.2.4, Terraform 1.17.1 + `dmacvicar/libvirt` 0.9.9, `openstack.cloud` 2.6.0, Ubuntu 24.04 cloud image.

Spec: `docs/superpowers/specs/2026-11-01-local-infra-design.md`.

## Global Constraints

- Everything runs in WSL (`Ubuntu-26.04`), in the clone at `~/projects/Aleph0-Cloud-Ecosystem`, branch `feat/2-local-infra`. Never under `/mnt/c`.
- All commands below run from the repo root unless a step says otherwise.
- Node OS: Ubuntu 24.04 (Noble). `kolla_base_distro: ubuntu`, `openstack_release: 2026.2`.
- Ceph image `quay.io/ceph/ceph:v20.2.4`, cephadm from `https://download.ceph.com/rpm-20.2.4/el9/noarch/cephadm`.
- Terraform 1.17.1, provider `dmacvicar/libvirt` `0.9.9`, `openstack.cloud` `2.6.0`.
- The node file never holds Kolla variable names under `all.vars` (Kolla's playbook `group_vars/all` outrank them). It uses `vip_address`, `if_mgmt`, `if_storage`, `if_external`; `globals.yml` maps them.
- Roles are exactly `control`, `network`, `compute`, `storage`; groups are `role_<role>`.
- Networks: mgmt 10.10.0.0/24 (VIP .10, nodes .11+), storage 10.20.0.0/24, external 10.30.0.0/24 (floating IPs .100–.199).
- Generated files live in `deploy/kolla/<env>/` and are gitignored; only `globals.yml` there is committed.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Everything written to the repo is in English.

---

### Task 1: Host prerequisites and the deploy toolchain

**Files:**
- Create: `deploy/requirements.txt`, `deploy/requirements.yml`
- Modify: `Makefile` (append), `.gitignore` (append)

**Interfaces:**
- Produces: `deploy/.venv/.done` Make target; `deploy/.venv/bin/{ansible-playbook,kolla-ansible,kolla-genpwd,openstack}`; collections `openstack.cloud`, `ansible.posix` installed.

- [ ] **Step 1: Bring the WSL clone up to date**

```bash
cd ~/projects/Aleph0-Cloud-Ecosystem
git fetch origin
git switch feat/2-local-infra
git log --oneline -1
```
Expected: the latest commit of `feat/2-local-infra`.

- [ ] **Step 2: Install host packages, Terraform and an SSH key (needs sudo)**

```bash
sudo apt-get update
sudo apt-get install -y qemu-system-x86 qemu-utils libvirt-daemon-system python3-venv unzip
sudo usermod -aG libvirt,kvm "$USER"
curl -fsSLo /tmp/terraform.zip https://releases.hashicorp.com/terraform/1.17.1/terraform_1.17.1_linux_amd64.zip
sudo unzip -o /tmp/terraform.zip terraform -d /usr/local/bin
[ -f ~/.ssh/id_ed25519 ] || ssh-keygen -t ed25519 -N '' -f ~/.ssh/id_ed25519
```
Then restart WSL so the new groups apply: from Windows, `wsl --terminate Ubuntu-26.04`, and open a new WSL shell.

- [ ] **Step 3: Verify the host**

```bash
id -nG | tr ' ' '\n' | grep -E '^(libvirt|kvm)$'
virsh -c qemu:///system list --all
terraform version | head -1
cat /sys/module/kvm_intel/parameters/nested 2>/dev/null || cat /sys/module/kvm_amd/parameters/nested
```
Expected: `libvirt` and `kvm` listed; an empty domain table (no error); `Terraform v1.17.1`; `Y`.

- [ ] **Step 4: Write `deploy/requirements.txt`**

```text
# Deploy toolchain for deploy/infra.sh. Kolla-Ansible pins ansible-core (2.20–2.21).
kolla-ansible @ git+https://opendev.org/openstack/kolla-ansible@stable/2026.2
openstacksdk
python-openstackclient
```

- [ ] **Step 5: Write `deploy/requirements.yml`**

```yaml
collections:
  - name: openstack.cloud
    version: 2.6.0
```

- [ ] **Step 6: Append the venv target to `Makefile`**

Append at the end of `Makefile`:

```make

# Deploy toolchain (WSL only). The marker is written last, so a failed install runs again.
deploy/.venv/.done: deploy/requirements.txt deploy/requirements.yml
	python3 -m venv deploy/.venv
	deploy/.venv/bin/pip install --upgrade pip
	deploy/.venv/bin/pip install -r deploy/requirements.txt
	deploy/.venv/bin/kolla-ansible install-deps
	deploy/.venv/bin/ansible-galaxy collection install -r deploy/requirements.yml
	touch $@
```

- [ ] **Step 7: Append to `.gitignore`**

```gitignore

# Deploy: toolchain, Terraform state and generated cluster files
deploy/.venv/
deploy/terraform/.terraform/
deploy/terraform/terraform.tfstate.d/
deploy/kolla/*/*
!deploy/kolla/*/globals.yml
```

- [ ] **Step 8: Build the toolchain and check it**

```bash
make deploy/.venv/.done
deploy/.venv/bin/kolla-ansible --version
deploy/.venv/bin/ansible --version | head -1
deploy/.venv/bin/ansible-galaxy collection list openstack.cloud ansible.posix
ls deploy/.venv/share/kolla-ansible/ansible/inventory/multinode deploy/.venv/share/kolla-ansible/etc_examples/kolla/passwords.yml
```
Expected: a kolla-ansible version; `ansible [core 2.20.x]` or `2.21.x`; both collections listed (openstack.cloud 2.6.0); both files exist. If pip fails on Python 3.14, stop and report the error; do not switch Python versions without asking.

- [ ] **Step 9: Commit**

```bash
git add deploy/requirements.txt deploy/requirements.yml Makefile .gitignore
git commit -m "build(deploy): add the Ansible and Kolla-Ansible toolchain (#2)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Node file, role groups and validation

**Files:**
- Create: `deploy/nodes/dev.yml`, `deploy/nodes/roles.yml`, `deploy/playbooks/validate.yml`, `deploy/infra.sh`
- Create: `deploy/nodes/testdata/{valid,two-controllers,no-network,too-few-storage,unknown-role}.yml`
- Modify: `Makefile` (append)

**Interfaces:**
- Consumes: `deploy/.venv/.done` (Task 1).
- Produces: groups `role_control|role_network|role_compute|role_storage`; host vars `ansible_host`, `storage_ip`, `node_roles`, `vm`; all-vars `ansible_user`, `vip_address`, `if_mgmt`, `if_storage`, `if_external`, `ceph_pool_size`, `ceph_devices`, `ceph_osd_memory_target`. `validate.yml` takes optional extra vars `cluster_env`, `cluster_dir` (orphan check runs only when `cluster_dir/clouds.yaml` exists). `deploy/infra.sh <cmd> <env> [node]` with `cmd=test`; `make infra-test`.

- [ ] **Step 1: Write the fixtures**

`deploy/nodes/testdata/valid.yml`:
```yaml
all:
  vars:
    ceph_pool_size: 1
  hosts:
    node1:
      ansible_host: 10.10.0.11
      storage_ip: 10.20.0.11
      node_roles: [control, network, compute, storage]
```

`deploy/nodes/testdata/two-controllers.yml`:
```yaml
all:
  vars:
    ceph_pool_size: 2
  hosts:
    node1:
      ansible_host: 10.10.0.11
      storage_ip: 10.20.0.11
      node_roles: [control, network, compute, storage]
    node2:
      ansible_host: 10.10.0.12
      storage_ip: 10.20.0.12
      node_roles: [control, compute, storage]
```

`deploy/nodes/testdata/no-network.yml`:
```yaml
all:
  vars:
    ceph_pool_size: 1
  hosts:
    node1:
      ansible_host: 10.10.0.11
      storage_ip: 10.20.0.11
      node_roles: [control, compute, storage]
```

`deploy/nodes/testdata/too-few-storage.yml`:
```yaml
all:
  vars:
    ceph_pool_size: 2
  hosts:
    node1:
      ansible_host: 10.10.0.11
      storage_ip: 10.20.0.11
      node_roles: [control, network, compute, storage]
    node2:
      ansible_host: 10.10.0.12
      storage_ip: 10.20.0.12
      node_roles: [compute]
```

`deploy/nodes/testdata/unknown-role.yml`:
```yaml
all:
  vars:
    ceph_pool_size: 1
  hosts:
    node1:
      ansible_host: 10.10.0.11
      storage_ip: 10.20.0.11
      node_roles: [control, network, compute, storage, gpu]
```

- [ ] **Step 2: Write `deploy/nodes/roles.yml`**

```yaml
# Turns each node's node_roles into role_* groups (role_control, role_network, ...).
plugin: ansible.builtin.constructed
keyed_groups:
  - key: node_roles
    prefix: role
```

- [ ] **Step 3: Write `deploy/infra.sh` with the `test` command**

```bash
#!/usr/bin/env bash
# Builds and changes the OpenStack + Ceph cluster described by deploy/nodes/<env>.yml.
# Usage: infra.sh apply|remove|destroy|test <env> [node]
set -euo pipefail
cd "$(dirname "$0")"

cmd=${1:?usage: infra.sh apply|remove|destroy|test <env> [node]}
env=${2:-dev}
nodes=nodes/$env.yml
cluster_dir=$PWD/kolla/$env
mkdir -p "$cluster_dir"

export PATH=$PWD/.venv/bin:$PATH

# Each bad fixture must be rejected by its own rule: <fixture>:<text of that rule's message>.
run_tests() {
  ansible-playbook -i nodes/testdata/valid.yml -i nodes/roles.yml playbooks/validate.yml
  local t f msg out
  for t in "two-controllers:use 1, 3 or 5" \
           "no-network:no node has the network role" \
           "too-few-storage:storage hosts for ceph_pool_size" \
           "unknown-role:needs ansible_host, storage_ip and node_roles"; do
    f=${t%%:*} msg=${t#*:}
    if out=$(ansible-playbook -i "nodes/testdata/$f.yml" -i nodes/roles.yml playbooks/validate.yml 2>&1); then
      echo "FAIL: nodes/testdata/$f.yml passed validation" >&2
      exit 1
    fi
    if ! grep -qF "$msg" <<<"$out"; then
      echo "FAIL: nodes/testdata/$f.yml was rejected for another reason:" >&2
      echo "$out" >&2
      exit 1
    fi
    echo "ok: nodes/testdata/$f.yml rejected ($msg)"
  done
}

case $cmd in
  test) run_tests ;;
  *) echo "unknown command: $cmd" >&2; exit 2 ;;
esac
```

```bash
chmod +x deploy/infra.sh
git add --chmod=+x deploy/infra.sh
```

- [ ] **Step 4: Append the `infra-test` target to `Makefile`**

```make

# Infrastructure (WSL only). ENV picks deploy/nodes/$(ENV).yml.
ENV ?= dev

.PHONY: infra-test
infra-test: deploy/.venv/.done
	deploy/infra.sh test $(ENV)
```

- [ ] **Step 5: Run it and watch it fail**

Run: `make infra-test`
Expected: FAIL, `ansible-playbook` reports that `playbooks/validate.yml` could not be found.

- [ ] **Step 6: Write `deploy/playbooks/validate.yml`**

```yaml
# Checks the node file before anything changes. With cluster_dir/clouds.yaml present (a
# cluster exists), also refuses nodes that were deleted from the file without node-remove.
- name: Validate the node file
  hosts: all:!localhost
  gather_facts: false
  vars:
    nodes: "{{ groups['all'] | difference(['localhost']) }}"
    known_roles: [control, network, compute, storage]
  tasks:
    - name: Rules
      run_once: true
      block:
        - name: Every node has an address, a storage address and known roles
          ansible.builtin.assert:
            that:
              - hostvars[item].ansible_host is defined
              - hostvars[item].storage_ip is defined
              - hostvars[item].node_roles | default([]) | length > 0
              - hostvars[item].node_roles | default([]) | difference(known_roles) | length == 0
            fail_msg: "{{ item }}: needs ansible_host, storage_ip and node_roles from {{ known_roles }}"
            quiet: true
          loop: "{{ nodes }}"

        - name: Odd number of controllers
          ansible.builtin.assert:
            that: groups['role_control'] | default([]) | length is odd
            fail_msg: "{{ groups['role_control'] | default([]) | length }} controllers: use 1, 3 or 5"

        - name: At least one network node
          ansible.builtin.assert:
            that: groups['role_network'] | default([]) | length > 0
            fail_msg: "no node has the network role"

        - name: At least one compute node
          ansible.builtin.assert:
            that: groups['role_compute'] | default([]) | length > 0
            fail_msg: "no node has the compute role"

        - name: Enough storage hosts for ceph_pool_size
          ansible.builtin.assert:
            that: groups['role_storage'] | default([]) | length >= ceph_pool_size | int
            fail_msg: "{{ groups['role_storage'] | default([]) | length }} storage hosts for ceph_pool_size {{ ceph_pool_size }}"

    - name: Orphan check
      run_once: true
      when: cluster_dir is defined
      block:
        - name: Look for the admin clouds.yaml
          ansible.builtin.stat:
            path: "{{ cluster_dir }}/clouds.yaml"
          delegate_to: localhost
          register: clouds

        # Every compute and network node runs an OVN agent; every controller is a Ceph host.
        - name: Hosts Neutron knows
          ansible.builtin.command: openstack network agent list -f value -c Host
          delegate_to: localhost
          register: agent_hosts
          changed_when: false
          when: clouds.stat.exists

        - name: Hosts Ceph knows
          ansible.builtin.command: cephadm shell -- ceph orch host ls --format json
          delegate_to: "{{ groups['role_control'][0] }}"
          become: true
          register: ceph_hosts
          changed_when: false
          when: clouds.stat.exists

        - name: No node left the file without node-remove
          ansible.builtin.assert:
            that: orphans | length == 0
            fail_msg: >-
              {{ orphans | join(', ') }}: still part of the cluster: run
              make node-remove ENV={{ cluster_env }} NODE=<name>, or put it back in the file
          vars:
            orphans: "{{ (agent_hosts.stdout_lines + (ceph_hosts.stdout | from_json | map(attribute='hostname') | list)) | unique | difference(nodes) }}"
          when: clouds.stat.exists
```

- [ ] **Step 7: Run the tests and watch them pass**

Run: `make infra-test`
Expected: the valid fixture's play ends with `failed=0`, then four lines `ok: nodes/testdata/<name>.yml rejected (<rule text>)`, exit code 0.

- [ ] **Step 8: Write the default dev node file `deploy/nodes/dev.yml`**

```yaml
# The dev cluster. Edit this file to add nodes or roles, then run `make infra-apply`.
# Roles: control (odd count), network, compute, storage. A `vm:` block makes it a libvirt VM.
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

- [ ] **Step 9: Check the dev file against the rules and the groups**

```bash
deploy/.venv/bin/ansible-playbook -i deploy/nodes/dev.yml -i deploy/nodes/roles.yml deploy/playbooks/validate.yml
deploy/.venv/bin/ansible-inventory -i deploy/nodes/dev.yml -i deploy/nodes/roles.yml --graph
```
Expected: `failed=0`; the graph shows `@role_control: node1`, `@role_network: node1`, `@role_compute: node1 node2 node3`, `@role_storage: node1 node2 node3`.

- [ ] **Step 10: Commit**

```bash
git add deploy/nodes deploy/playbooks/validate.yml deploy/infra.sh Makefile
git commit -m "feat(deploy): describe the cluster in a node file and validate it (#2)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Dev machines with Terraform

**Files:**
- Create: `deploy/terraform/main.tf`, `deploy/terraform/.terraform.lock.hcl` (generated by `terraform init`), `deploy/playbooks/hosts.yml`
- Modify: `deploy/infra.sh`, `Makefile` (append)

**Interfaces:**
- Consumes: the node file (Task 2).
- Produces: Terraform `variable "nodes_file"`; resources `libvirt_domain.node[<name>]`, `libvirt_volume.system[<name>]`, `libvirt_volume.ceph[<name>]`, `libvirt_volume.init[<name>]`, `libvirt_cloudinit_disk.init[<name>]`; VMs with `eth0/eth1/eth2`, `/dev/vdb`, user `ubuntu`. `infra.sh` functions `play <playbook> [args]`, `tf <args>`, `machines`, `apply`, `destroy`, array `inv`. `make infra-apply`, `make infra-destroy`.

- [ ] **Step 1: Write `deploy/terraform/main.tf`**

```hcl
# Dev nodes as libvirt VMs, one per host with a `vm:` block in the node file.
terraform {
  required_version = ">= 1.17"
  required_providers {
    libvirt = {
      source  = "dmacvicar/libvirt"
      version = "0.9.9"
    }
  }
}

provider "libvirt" {
  uri = "qemu:///system"
}

variable "nodes_file" {
  type        = string
  description = "Absolute path of the node file (deploy/nodes/<env>.yml)."
}

locals {
  inventory = yamldecode(file(var.nodes_file))
  vms       = { for name, n in local.inventory.all.hosts : name => n if try(n.vm, null) != null }
  ceph_vms  = { for name, n in local.vms : name => n if try(n.vm.ceph_disk_gb, 0) > 0 }
  user      = local.inventory.all.vars.ansible_user
  gib       = 1024 * 1024 * 1024

  networks = {
    mgmt    = { mac = "10", prefix = "10.10.0", nat = true }
    storage = { mac = "20", prefix = "10.20.0", nat = false }
    ext     = { mac = "30", prefix = "10.30.0", nat = true }
  }
  nic_order = ["mgmt", "storage", "ext"]

  # 52:54:00:<net>:00:<last octet of ansible_host>, matched by cloud-init to name the NICs.
  macs = {
    for name, n in local.vms : name => {
      for net, v in local.networks : net => format("52:54:00:%s:00:%02x", v.mac, tonumber(split(".", n.ansible_host)[3]))
    }
  }
}

resource "libvirt_pool" "aleph0" {
  name   = "aleph0"
  type   = "dir"
  target = { path = "/var/lib/libvirt/images/aleph0" }
  create = { build = true, start = true, autostart = true }
}

resource "libvirt_network" "net" {
  for_each  = local.networks
  name      = "aleph0-${each.key}"
  autostart = true
  forward   = each.value.nat ? { mode = "nat" } : null
  ips       = [{ address = "${each.value.prefix}.1", prefix = 24 }]
}

resource "libvirt_volume" "noble" {
  name   = "noble-base.qcow2"
  pool   = libvirt_pool.aleph0.name
  target = { format = { type = "qcow2" } }
  create = { content = { url = "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-amd64.img" } }
}

resource "libvirt_volume" "system" {
  for_each = local.vms
  name     = "${each.key}-system.qcow2"
  pool     = libvirt_pool.aleph0.name
  target   = { format = { type = "qcow2" } }
  capacity = 40 * local.gib
  backing_store = {
    path   = libvirt_volume.noble.path
    format = { type = "qcow2" }
  }
}

resource "libvirt_volume" "ceph" {
  for_each = local.ceph_vms
  name     = "${each.key}-ceph.qcow2"
  pool     = libvirt_pool.aleph0.name
  target   = { format = { type = "qcow2" } }
  capacity = each.value.vm.ceph_disk_gb * local.gib
}

resource "libvirt_cloudinit_disk" "init" {
  for_each  = local.vms
  name      = "${each.key}-init"
  meta_data = yamlencode({ "instance-id" = each.key, "local-hostname" = each.key })
  user_data = "#cloud-config\n${yamlencode({
    users = [{
      name                = local.user
      sudo                = "ALL=(ALL) NOPASSWD:ALL"
      shell               = "/bin/bash"
      ssh_authorized_keys = [trimspace(file(pathexpand("~/.ssh/id_ed25519.pub")))]
    }]
  })}"
  network_config = yamlencode({
    version = 2
    ethernets = {
      eth0 = {
        match       = { macaddress = local.macs[each.key].mgmt }
        "set-name"  = "eth0"
        addresses   = ["${each.value.ansible_host}/24"]
        routes      = [{ to = "default", via = "10.10.0.1" }]
        nameservers = { addresses = ["10.10.0.1"] }
      }
      eth1 = {
        match      = { macaddress = local.macs[each.key].storage }
        "set-name" = "eth1"
        addresses  = ["${each.value.storage_ip}/24"]
      }
      eth2 = {
        match      = { macaddress = local.macs[each.key].ext }
        "set-name" = "eth2"
        dhcp4      = false
      }
    }
  })
}

resource "libvirt_volume" "init" {
  for_each = local.vms
  name     = "${each.key}-init.iso"
  pool     = libvirt_pool.aleph0.name
  target   = { format = { type = "raw" } }
  create   = { content = { url = libvirt_cloudinit_disk.init[each.key].path } }
}

resource "libvirt_domain" "node" {
  for_each    = local.vms
  name        = "aleph0-${each.key}"
  type        = "kvm"
  vcpu        = each.value.vm.vcpus
  memory      = each.value.vm.memory_gb
  memory_unit = "GiB"
  running     = true
  cpu         = { mode = "host-passthrough" }
  os          = { type = "hvm", type_arch = "x86_64", type_machine = "q35" }

  devices = {
    disks = concat(
      [{
        device = "disk"
        source = { volume = { pool = libvirt_pool.aleph0.name, volume = libvirt_volume.system[each.key].name } }
        target = { bus = "virtio", dev = "vda" }
        driver = { type = "qcow2" }
      }],
      contains(keys(local.ceph_vms), each.key) ? [{
        device = "disk"
        source = { volume = { pool = libvirt_pool.aleph0.name, volume = libvirt_volume.ceph[each.key].name } }
        target = { bus = "virtio", dev = "vdb" }
        driver = { type = "qcow2" }
      }] : [],
      [{
        device = "cdrom"
        source = { volume = { pool = libvirt_pool.aleph0.name, volume = libvirt_volume.init[each.key].name } }
        target = { bus = "sata", dev = "sda" }
        driver = { type = "raw" }
      }],
    )

    interfaces = [for net in local.nic_order : {
      type   = "network"
      model  = { type = "virtio" }
      mac    = { address = local.macs[each.key][net] }
      source = { network = { network = libvirt_network.net[net].name } }
    }]
  }
}
```

- [ ] **Step 2: Initialise and validate**

```bash
cd deploy/terraform && terraform init -input=false && terraform validate && cd -
```
Expected: `Success! The configuration is valid.` and a new `deploy/terraform/.terraform.lock.hcl`. If validate reports an attribute error, check the attribute in the provider docs for 0.9.9 (`https://github.com/dmacvicar/terraform-provider-libvirt/tree/v0.9.9/docs/resources`) and fix `main.tf`; keep the resource names above.

- [ ] **Step 3: Write `deploy/playbooks/hosts.yml`**

```yaml
# Waits for every node and lists all nodes in every node's /etc/hosts, so a new node is known
# everywhere. Kolla's own /etc/hosts handling is off (customize_etc_hosts: no).
- name: Prepare the nodes
  hosts: all:!localhost
  gather_facts: false
  become: true
  tasks:
    - name: Wait for SSH
      ansible.builtin.wait_for_connection:
        timeout: 600

    - name: Wait for cloud-init on VMs
      ansible.builtin.command: cloud-init status --wait
      changed_when: false
      when: vm is defined

    - name: Drop the 127.0.1.1 hostname line (Kolla needs the hostname on the management IP)
      ansible.builtin.lineinfile:
        path: /etc/hosts
        regexp: '^127\.0\.1\.1\s'
        state: absent

    - name: List every node
      ansible.builtin.blockinfile:
        path: /etc/hosts
        marker: "# {mark} aleph0 nodes"
        block: |
          {% for h in groups['all'] | difference(['localhost']) %}
          {{ hostvars[h].ansible_host }} {{ h }}
          {% endfor %}
```

- [ ] **Step 4: Add `apply` (first part) and `destroy` to `deploy/infra.sh`**

Replace everything from `export PATH=$PWD/.venv/bin:$PATH` to the end of the file with:

```bash
export PATH=$PWD/.venv/bin:$PATH
if [[ $env == dev ]]; then
  # Dev VMs are recreated with the same addresses, so their host keys change.
  export ANSIBLE_HOST_KEY_CHECKING=False
  export ANSIBLE_SSH_ARGS="-o ControlMaster=auto -o ControlPersist=60s -o UserKnownHostsFile=/dev/null"
fi

inv=(-i "$nodes" -i nodes/roles.yml)

play() { ansible-playbook "${inv[@]}" -e "cluster_env=$env cluster_dir=$cluster_dir" "playbooks/$1" "${@:2}"; }
tf() { (cd terraform && terraform "$@"); }
has_vms() { grep -q '^ *vm:' "$nodes"; }

machines() {
  has_vms || return 0
  tf init -input=false >/dev/null
  tf workspace select -or-create "$env"
  tf apply -input=false -auto-approve -var "nodes_file=$PWD/$nodes"
}

apply() {
  play validate.yml
  machines
  play hosts.yml
}

destroy() {
  if [[ $env != dev ]]; then
    echo "infra-destroy only runs for dev" >&2
    exit 1
  fi
  tf init -input=false >/dev/null
  tf workspace select -or-create dev
  tf destroy -input=false -auto-approve -var "nodes_file=$PWD/$nodes"
  find "$cluster_dir" -mindepth 1 -maxdepth 1 ! -name globals.yml -exec rm -rf {} +
}

# Each bad fixture must be rejected by its own rule: <fixture>:<text of that rule's message>.
run_tests() {
  ansible-playbook -i nodes/testdata/valid.yml -i nodes/roles.yml playbooks/validate.yml
  local t f msg out
  for t in "two-controllers:use 1, 3 or 5" \
           "no-network:no node has the network role" \
           "too-few-storage:storage hosts for ceph_pool_size" \
           "unknown-role:needs ansible_host, storage_ip and node_roles"; do
    f=${t%%:*} msg=${t#*:}
    if out=$(ansible-playbook -i "nodes/testdata/$f.yml" -i nodes/roles.yml playbooks/validate.yml 2>&1); then
      echo "FAIL: nodes/testdata/$f.yml passed validation" >&2
      exit 1
    fi
    if ! grep -qF "$msg" <<<"$out"; then
      echo "FAIL: nodes/testdata/$f.yml was rejected for another reason:" >&2
      echo "$out" >&2
      exit 1
    fi
    echo "ok: nodes/testdata/$f.yml rejected ($msg)"
  done
  tf init -backend=false -input=false >/dev/null
  tf validate
}

case $cmd in
  apply) apply ;;
  destroy) destroy ;;
  test) run_tests ;;
  *) echo "unknown command: $cmd" >&2; exit 2 ;;
esac
```

- [ ] **Step 5: Add the Make targets**

Append to `Makefile`, after the `infra-test` target:

```make

.PHONY: infra-apply infra-destroy
infra-apply: deploy/.venv/.done
	deploy/infra.sh apply $(ENV)

infra-destroy:
	deploy/infra.sh destroy $(ENV)
```

- [ ] **Step 6: Bring up the VMs**

Run: `make infra-apply`
Expected: Terraform creates 3 networks, the pool, 3 domains; `hosts.yml` ends with `failed=0` for node1–node3.

- [ ] **Step 7: Check the VMs**

```bash
for n in 11 12 13; do
  ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@10.10.0.$n \
    'hostname; ip -br addr show | grep -E "^eth[0-2]"; lsblk -dn -o NAME,SIZE | grep vdb; grep aleph0 -A4 /etc/hosts; curl -sI https://quay.io | head -1'
done
```
Expected per node: hostname `nodeN`; `eth0` with 10.10.0.N/24, `eth1` with 10.20.0.N/24, `eth2` UP without an IPv4 address; `vdb 50G`; the three node lines in `/etc/hosts`; `HTTP/2 200` (internet through the NAT network). If `curl` fails, libvirt's NAT does not work inside WSL: stop and report the output of `sudo nft list ruleset | grep -i libvirt`.

- [ ] **Step 8: Check destroy and re-create**

```bash
make infra-destroy
virsh -c qemu:///system list --all | grep aleph0 || echo "no aleph0 domains"
make infra-test
make infra-apply
```
Expected: `no aleph0 domains`; `infra-test` passes including `Success! The configuration is valid.`; `infra-apply` brings the three VMs back.

- [ ] **Step 9: Commit**

```bash
git add deploy/terraform/main.tf deploy/terraform/.terraform.lock.hcl deploy/playbooks/hosts.yml deploy/infra.sh Makefile
git commit -m "feat(deploy): create dev nodes as libvirt VMs from the node file (#2)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Kolla inventory, globals, pending nodes and Docker

**Files:**
- Create: `deploy/kolla/services.ini`, `deploy/kolla/dev/globals.yml`, `deploy/playbooks/pending.yml`, `deploy/playbooks/markers.yml`
- Modify: `deploy/infra.sh`

**Interfaces:**
- Consumes: `inv`, `play`, `apply` (Task 3); `deploy/.venv/share/kolla-ansible/...` (Task 1).
- Produces: Kolla groups `control`, `network`, `compute`, `monitoring`, `storage`, `deployment` (localhost), `baremetal` ⊇ `role_storage`. `deploy/kolla/<env>/pending.env` with `PENDING=<csv>` and `PENDING_CONTROL=yes|no`. `infra.sh` function `ka <kolla-subcommand> [args]` and `passwords`; `markers.yml` (run with `--limit "$PENDING"`).

- [ ] **Step 1: Write the header of `deploy/kolla/services.ini`**

```ini
# Kolla's top-level groups, filled from the role_* groups that deploy/nodes/roles.yml builds out
# of each node's node_roles. Everything from the first [baremetal:children] on is copied from
# Kolla-Ansible stable/2026.2 ansible/inventory/multinode; refresh it when Kolla is upgraded.
[role_control]
[role_network]
[role_compute]
[role_storage]

[control:children]
role_control

[network:children]
role_network

[compute:children]
role_compute

[monitoring:children]
role_control

# Kolla's storage group runs cinder-volume/backup, which with Ceph belong on the controllers.
[storage:children]
role_control

[deployment]
localhost ansible_connection=local ansible_python_interpreter="{{ ansible_playbook_python }}"

# Storage-only nodes run no Kolla services, but bootstrap-servers installs Docker for cephadm.
[baremetal:children]
role_storage

```

- [ ] **Step 2: Append Kolla's service mapping**

```bash
sed -n '/^\[baremetal:children\]/,$p' deploy/.venv/share/kolla-ansible/ansible/inventory/multinode >> deploy/kolla/services.ini
```

- [ ] **Step 3: Check the groups**

```bash
I="-i deploy/nodes/dev.yml -i deploy/nodes/roles.yml -i deploy/kolla/services.ini"
deploy/.venv/bin/ansible-inventory $I --graph control
deploy/.venv/bin/ansible-inventory $I --graph compute
deploy/.venv/bin/ansible-inventory $I --graph baremetal
deploy/.venv/bin/ansible-inventory $I --graph nova-compute
deploy/.venv/bin/ansible-inventory $I --graph deployment
```
Expected: `control` → node1; `compute` → node1, node2, node3; `baremetal` contains node1–node3; `nova-compute` → node1–node3; `deployment` → localhost. No "undefined group" error. If the second `[baremetal:children]` section is rejected, instead delete the header's `[baremetal:children]` section and run `sed -i '0,/^\[baremetal:children\]/s//&\nrole_storage/' deploy/kolla/services.ini`, then repeat this step.

- [ ] **Step 4: Write `deploy/kolla/dev/globals.yml`**

```yaml
---
# Kolla-Ansible settings for the dev cluster. Node-specific values come from the node file
# (deploy/nodes/dev.yml) under our own names, so any node can override them.
kolla_base_distro: "ubuntu"
openstack_release: "2026.2"

kolla_internal_vip_address: "{{ vip_address }}"
network_interface: "{{ if_mgmt }}"
storage_interface: "{{ if_storage }}"
neutron_external_interface: "{{ if_external }}"
neutron_plugin_agent: "ovn"

# A VIP from day one, so endpoints stay put when controllers 2 and 3 join.
enable_haproxy: "yes"
# Our hosts.yml manages /etc/hosts; rerunning Kolla's on live nodes can restart Docker.
customize_etc_hosts: "no"

# Third nesting level (Windows → WSL2 → node → VM). Production: "kvm".
nova_compute_virt_type: "qemu"

glance_backend_ceph: "yes"
cinder_backend_ceph: "yes"
nova_backend_ceph: "yes"
enable_cinder: "yes"

enable_horizon: "no"         # we have our own portal
enable_octavia: "no"         # with the load balancer step
enable_ceph_rgw: "no"        # Ceph runs RGW; Keystone/HAProxy integration comes with the S3 step
enable_fluentd: "no"         # saves memory in dev
```

- [ ] **Step 5: Write `deploy/playbooks/pending.yml`**

```yaml
# A node is pending when its marker (/etc/aleph0/roles, written by markers.yml after a
# successful deploy) is missing or lists other roles than the node file.
- name: Read the markers
  hosts: all:!localhost
  gather_facts: false
  become: true
  tasks:
    - name: Read /etc/aleph0/roles
      ansible.builtin.slurp:
        src: /etc/aleph0/roles
      register: marker
      failed_when: false

    - name: Compare with the node file
      ansible.builtin.set_fact:
        pending: "{{ marker.content is not defined or (marker.content | b64decode | from_json) != (node_roles | sort) }}"

- name: Write pending.env
  hosts: localhost
  gather_facts: false
  vars:
    pending_nodes: "{{ groups['all'] | difference(['localhost']) | map('extract', hostvars) | selectattr('pending') | map(attribute='inventory_hostname') | list }}"
  tasks:
    - name: Write the pending list
      ansible.builtin.copy:
        dest: "{{ cluster_dir }}/pending.env"
        mode: "0644"
        content: |
          PENDING={{ pending_nodes | join(',') }}
          PENDING_CONTROL={{ 'yes' if pending_nodes | intersect(groups['role_control']) else 'no' }}
```

- [ ] **Step 6: Write `deploy/playbooks/markers.yml`**

```yaml
# Records the roles a node got, after a successful deploy. Run with --limit <pending nodes>.
- name: Write the markers
  hosts: all:!localhost
  gather_facts: false
  become: true
  tasks:
    - name: Create /etc/aleph0
      ansible.builtin.file:
        path: /etc/aleph0
        state: directory
        mode: "0755"

    - name: Write /etc/aleph0/roles
      ansible.builtin.copy:
        dest: /etc/aleph0/roles
        mode: "0644"
        content: "{{ node_roles | sort | to_json }}\n"
```

- [ ] **Step 7: Extend `deploy/infra.sh`**

Replace the line `inv=(-i "$nodes" -i nodes/roles.yml)` with:

```bash
inv=(-i "$nodes" -i nodes/roles.yml -i kolla/services.ini)
```

After the `has_vms` line add:

```bash
ka() { kolla-ansible "$1" "${inv[@]}" --configdir "$cluster_dir" --passwords "$cluster_dir/passwords.yml" "${@:2}"; }

passwords() {
  if [[ ! -f $cluster_dir/passwords.yml ]]; then
    cp .venv/share/kolla-ansible/etc_examples/kolla/passwords.yml "$cluster_dir/passwords.yml"
    kolla-genpwd -p "$cluster_dir/passwords.yml"
  fi
}
```

Replace the `apply` function with:

```bash
apply() {
  play validate.yml
  machines
  play hosts.yml
  play pending.yml
  # shellcheck source=/dev/null
  source "$cluster_dir/pending.env"
  limit=$PENDING
  if [[ $PENDING_CONTROL == yes ]]; then
    limit+=,role_control   # Kolla wants every controller in the limit when one is added
  fi
  if [[ -n $PENDING ]]; then
    passwords
    ka bootstrap-servers --limit "$limit"
  fi
}
```

- [ ] **Step 8: Run it**

Run: `make infra-apply`
Expected: `pending.yml` writes `deploy/kolla/dev/pending.env`; `bootstrap-servers` ends with `failed=0` for node1–node3.

- [ ] **Step 9: Check**

```bash
cat deploy/kolla/dev/pending.env
for n in 11 12 13; do ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@10.10.0.$n 'sudo docker info --format "{{.ServerVersion}}"'; done
git status --short deploy/kolla
```
Expected: `PENDING=node1,node2,node3` and `PENDING_CONTROL=yes`; a Docker version from each node; `git status` shows only `services.ini` and `dev/globals.yml` (passwords and pending.env are ignored).

- [ ] **Step 10: Commit**

```bash
git add deploy/kolla/services.ini deploy/kolla/dev/globals.yml deploy/playbooks/pending.yml deploy/playbooks/markers.yml deploy/infra.sh
git commit -m "feat(deploy): map node roles onto Kolla groups and prepare pending nodes (#2)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Ceph

**Files:**
- Create: `deploy/playbooks/group_vars/all.yml`, `deploy/playbooks/templates/cluster.yml.j2`, `deploy/playbooks/templates/pools.sh.j2`, `deploy/playbooks/ceph.yml`
- Modify: `deploy/infra.sh`

**Interfaces:**
- Consumes: Docker on all control/storage nodes (Task 4); `storage_ip`, `ceph_devices`, `ceph_pool_size`, `ceph_osd_memory_target`, `ansible_user` (Task 2).
- Produces: a Ceph cluster administered from `groups['role_control'][0]` via `cephadm shell`; pools `volumes`, `images`, `vms`, `backups`; users `client.glance`, `client.cinder`, `client.cinder-backup`; files under `deploy/kolla/<env>/config/` in Kolla's external-Ceph layout plus `config/glance.conf`.

- [ ] **Step 1: Write `deploy/playbooks/group_vars/all.yml`**

```yaml
ceph_version: "20.2.4"
ceph_image: "quay.io/ceph/ceph:v{{ ceph_version }}"
```

- [ ] **Step 2: Write `deploy/playbooks/templates/cluster.yml.j2`**

```jinja
{# cephadm service specs for the whole cluster, applied with `ceph orch apply -i`. #}
{% set storage = groups['role_storage'] | default([]) %}
{% for h in ceph_hosts %}
---
service_type: host
hostname: {{ h }}
addr: {{ hostvars[h].storage_ip }}
labels:
{% if h in groups['role_control'] %}
  - mon
  - mgr
  - rgw
  - _admin
{% endif %}
{% if h in storage %}
  - osd
{% endif %}
{% endfor %}
---
service_type: mon
placement:
  label: mon
---
service_type: mgr
placement:
  label: mgr
---
service_type: rgw
service_id: default
placement:
  label: rgw
spec:
  rgw_frontend_port: 8080
{% for h in storage %}
---
service_type: osd
service_id: {{ h }}
placement:
  hosts:
    - {{ h }}
spec:
  data_devices:
    paths:
{% for d in hostvars[h].ceph_devices %}
      - {{ d }}
{% endfor %}
{% endfor %}
```

- [ ] **Step 3: Write `deploy/playbooks/templates/pools.sh.j2`**

```bash
#!/bin/bash
# Runs inside `cephadm shell` (rendered by ceph.yml). Safe to rerun.
set -euo pipefail
{% if ceph_osd_memory_target is defined %}
ceph config set osd osd_memory_target {{ ceph_osd_memory_target }}
{% endif %}
{% if ceph_pool_size | int == 1 %}
ceph config set global mon_allow_pool_size_one true
{% endif %}
for pool in volumes images vms backups; do
  ceph osd pool create "$pool"
  rbd pool init "$pool"
  ceph osd pool set "$pool" size {{ ceph_pool_size }} --yes-i-really-mean-it
  ceph osd pool set "$pool" min_size {{ (ceph_pool_size | int) - (ceph_pool_size | int // 2) }}
done
ceph auth get-or-create client.glance mon 'profile rbd' osd 'profile rbd pool=images' mgr 'profile rbd pool=images' >/dev/null
ceph auth get-or-create client.cinder mon 'profile rbd' osd 'profile rbd pool=volumes, profile rbd pool=vms, profile rbd-read-only pool=images' mgr 'profile rbd pool=volumes, profile rbd pool=vms' >/dev/null
ceph auth get-or-create client.cinder-backup mon 'profile rbd' osd 'profile rbd pool=backups' mgr 'profile rbd pool=backups' >/dev/null
# Kolla merges ceph.conf as INI; strip the generated file's leading tabs.
ceph config generate-minimal-conf | sed 's/^[[:space:]]*//' > /specs/out/ceph.conf
for user in glance cinder cinder-backup; do
  ceph auth get "client.$user" > "/specs/out/ceph.client.$user.keyring"
done
```

- [ ] **Step 4: Write `deploy/playbooks/ceph.yml`**

```yaml
# Ceph via cephadm, administered from the first controller. Safe to rerun: bootstrap runs once,
# the specs and the pool script are declarative.
- name: Install cephadm on the controllers
  hosts: role_control
  gather_facts: false
  become: true
  tasks:
    - name: Download cephadm
      ansible.builtin.get_url:
        url: "https://download.ceph.com/rpm-{{ ceph_version }}/el9/noarch/cephadm"
        dest: /usr/local/bin/cephadm
        mode: "0755"

- name: Bootstrap Ceph on the first controller
  hosts: role_control[0]
  gather_facts: false
  become: true
  tasks:
    - name: Bootstrap
      ansible.builtin.command:
        cmd: >-
          cephadm --image {{ ceph_image }} bootstrap
          --mon-ip {{ storage_ip }}
          --ssh-user {{ ansible_user }}
          --skip-monitoring-stack
          --skip-dashboard
        creates: /etc/ceph/ceph.conf

    - name: Read the cephadm SSH key
      ansible.builtin.slurp:
        src: /etc/ceph/ceph.pub
      register: ceph_pub

- name: Let cephadm reach every Ceph host
  hosts: role_control:role_storage
  gather_facts: false
  become: true
  tasks:
    - name: Authorize the cephadm key
      ansible.posix.authorized_key:
        user: "{{ ansible_user }}"
        key: "{{ hostvars[groups['role_control'][0]].ceph_pub.content | b64decode }}"

- name: Place daemons, create pools and users, hand the keys to Kolla
  hosts: role_control[0]
  gather_facts: false
  become: true
  vars:
    ceph_hosts: "{{ groups['role_control'] | union(groups['role_storage'] | default([])) }}"
    expected_osds: "{{ groups['role_storage'] | default([]) | map('extract', hostvars, 'ceph_devices') | map('length') | sum }}"
    shell: cephadm shell -m /etc/aleph0/ceph:/specs --
  tasks:
    - name: Create the spec folder
      ansible.builtin.file:
        path: /etc/aleph0/ceph/out
        state: directory
        mode: "0700"

    - name: Write the cluster spec and the pool script
      ansible.builtin.template:
        src: "templates/{{ item }}.j2"
        dest: "/etc/aleph0/ceph/{{ item }}"
        mode: "0600"
      loop: [cluster.yml, pools.sh]

    - name: Apply the cluster spec
      ansible.builtin.command: "{{ shell }} ceph orch apply -i /specs/cluster.yml"
      changed_when: true

    - name: Wait until every Ceph disk is an up OSD
      ansible.builtin.command: cephadm shell -- ceph osd stat -f json
      register: osd_stat
      changed_when: false
      retries: 60
      delay: 10
      until: (osd_stat.stdout | from_json).num_up_osds >= expected_osds | int

    - name: Create pools and users, export ceph.conf and keyrings
      ansible.builtin.command: "{{ shell }} bash /specs/pools.sh"
      changed_when: true

    - name: Copy ceph.conf and keyrings into Kolla's config
      ansible.builtin.fetch:
        src: "/etc/aleph0/ceph/out/{{ item.src }}"
        dest: "{{ cluster_dir }}/config/{{ item.dest }}"
        flat: true
      loop:
        - { src: ceph.conf, dest: glance/ceph.conf }
        - { src: ceph.client.glance.keyring, dest: glance/ceph.client.glance.keyring }
        - { src: ceph.conf, dest: cinder/ceph.conf }
        - { src: ceph.client.cinder.keyring, dest: cinder/cinder-volume/ceph.client.cinder.keyring }
        - { src: ceph.client.cinder.keyring, dest: cinder/cinder-backup/ceph.client.cinder.keyring }
        - { src: ceph.client.cinder-backup.keyring, dest: cinder/cinder-backup/ceph.client.cinder-backup.keyring }
        - { src: ceph.conf, dest: nova/ceph.conf }
        - { src: ceph.client.cinder.keyring, dest: nova/ceph.client.cinder.keyring }

    - name: Let Glance hand out copy-on-write clones
      ansible.builtin.copy:
        dest: "{{ cluster_dir }}/config/glance.conf"
        mode: "0644"
        content: |
          [DEFAULT]
          show_image_direct_url = True
      delegate_to: localhost
      become: false
```

- [ ] **Step 5: Run Ceph from `apply`, on every run**

In `deploy/infra.sh`, add `play ceph.yml` after the `if [[ -n $PENDING ]]` block, so the end of `apply` reads:

```bash
  if [[ -n $PENDING ]]; then
    passwords
    ka bootstrap-servers --limit "$limit"
  fi
  play ceph.yml   # every run, so ceph_pool_size and spec changes apply even when no node changed
}
```

- [ ] **Step 6: Run it**

Run: `make infra-apply`
Expected: `ceph.yml` ends with `failed=0`. Bootstrap takes a few minutes; the OSD wait may retry several times.

- [ ] **Step 7: Check Ceph**

```bash
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@10.10.0.11 \
  'sudo cephadm shell -- ceph -s; sudo cephadm shell -- ceph osd pool ls detail | grep -E "volumes|images|vms|backups"; sudo cephadm shell -- ceph orch host ls'
find deploy/kolla/dev/config -type f | sort
```
Expected: `health: HEALTH_OK`, `3 osds: 3 up, 3 in`, 1 mon, 1 mgr, 1 rgw; the four pools with `size 3 min_size 2`; hosts node1 (labels `mon,mgr,rgw,_admin,osd`), node2 and node3 (`osd`); the 8 files from the fetch loop plus `glance.conf`. If health shows a warning, report it before continuing.

- [ ] **Step 8: Rerun to prove idempotency**

Run: `make infra-apply`
Expected: completes; `ceph -s` still `HEALTH_OK`, still 3 OSDs (no duplicates).

- [ ] **Step 9: Commit**

```bash
git add deploy/playbooks/group_vars deploy/playbooks/templates deploy/playbooks/ceph.yml deploy/infra.sh
git commit -m "feat(deploy): deploy Ceph with cephadm from the node roles (#2)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: OpenStack with Kolla-Ansible

**Files:**
- Modify: `deploy/infra.sh`

**Interfaces:**
- Consumes: `ka`, `passwords`, `limit`, `PENDING`, `PENDING_CONTROL` (Task 4); Ceph config (Task 5).
- Produces: a running OpenStack behind `vip_address`; `deploy/kolla/<env>/clouds.yaml` with cloud `kolla-admin`; markers on deployed nodes. `infra.sh` exports `OS_CLIENT_CONFIG_FILE` and `OS_CLOUD=kolla-admin`.

- [ ] **Step 1: Export the admin cloud**

In `deploy/infra.sh`, after `export PATH=$PWD/.venv/bin:$PATH` add:

```bash
export OS_CLIENT_CONFIG_FILE=$cluster_dir/clouds.yaml OS_CLOUD=kolla-admin
```

- [ ] **Step 2: Add the Kolla steps and markers to `apply`**

After `play ceph.yml` in `apply`, add:

```bash
  if [[ -n $PENDING ]]; then
    ka prechecks --limit "$limit"
    ka pull --limit "$limit"
    if [[ $PENDING_CONTROL == yes ]]; then
      ka deploy   # HAProxy, Galera, RabbitMQ and the OVN databases must learn the new controller
    else
      ka deploy --limit "$limit"
    fi
    if [[ ! -f $cluster_dir/clouds.yaml ]]; then
      ka post-deploy
    fi
    play markers.yml --limit "$PENDING"
  fi
```

- [ ] **Step 3: Run it**

Run: `make infra-apply`
Expected: `prechecks`, `pull` and `deploy` end with `failed=0`; `post-deploy` writes `deploy/kolla/dev/clouds.yaml`. The first deploy takes 30–60 minutes. If `prechecks` fails, report its message; do not skip prechecks.

- [ ] **Step 4: Check OpenStack**

```bash
export OS_CLIENT_CONFIG_FILE=$PWD/deploy/kolla/dev/clouds.yaml OS_CLOUD=kolla-admin
deploy/.venv/bin/openstack endpoint list --interface internal -c "Service Name" -c URL
deploy/.venv/bin/openstack compute service list
deploy/.venv/bin/openstack network agent list
deploy/.venv/bin/openstack volume service list
cat deploy/kolla/dev/pending.env
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ubuntu@10.10.0.12 cat /etc/aleph0/roles
```
Expected: every URL on `10.10.0.10`; `nova-compute` up on node1–node3; OVN controller agents for node1–node3 (node1 as gateway); `cinder-volume` and `cinder-backup` up; the marker on node2 is `["compute", "storage"]`.

- [ ] **Step 5: Rerun: nothing pending**

Run: `make infra-apply && cat deploy/kolla/dev/pending.env`
Expected: finishes in a few minutes; Ceph re-applies, Kolla does not run; `PENDING=` (empty), `PENDING_CONTROL=no`.

- [ ] **Step 6: Commit**

```bash
git add deploy/infra.sh
git commit -m "feat(deploy): deploy OpenStack with Kolla-Ansible on pending nodes (#2)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: OpenStack init (acceptance 1–3)

**Files:**
- Create: `deploy/playbooks/openstack-init.yml`
- Modify: `deploy/infra.sh`

**Interfaces:**
- Consumes: `clouds.yaml`, `OS_CLOUD` env (Task 6).
- Produces: network `public` (flat, `physnet1`, subnet 10.30.0.0/24, pool .100–.199); flavors `s.tiny`, `s.small`; image `cirros`; project `dev` with `dev-net` (192.168.100.0/24) and `dev-router`; user `aleph0-api` (`member` on `dev`), password in `deploy/kolla/<env>/credentials/aleph0-api`. Kolla's own `admin` (local, not federated) is the emergency admin.

- [ ] **Step 1: Check the module options for this collection version**

```bash
for m in network subnet router compute_flavor image project identity_user role_assignment security_group_rule; do
  deploy/.venv/bin/ansible-doc "openstack.cloud.$m" | grep -E '^[-=] ' | tr '\n' ' '; echo; echo "== $m"
done
```
Expected: the option names. If any option used in Step 2 is missing for its module, use the name `ansible-doc` lists for the same setting.

- [ ] **Step 2: Write `deploy/playbooks/openstack-init.yml`**

```yaml
# Initial OpenStack resources. Idempotent; uses the kolla-admin cloud from OS_CLOUD.
- name: Initial OpenStack resources
  hosts: localhost
  gather_facts: false
  vars:
    cirros_version: "0.6.3"
    cirros_file: "{{ cluster_dir }}/cirros-{{ cirros_version }}-x86_64-disk.img"
    api_password: "{{ lookup('ansible.builtin.password', cluster_dir ~ '/credentials/aleph0-api length=32 chars=ascii_letters,digits') }}"
  tasks:
    - name: External network for floating IPs
      openstack.cloud.network:
        name: public
        external: true
        provider_network_type: flat
        provider_physical_network: physnet1

    - name: External subnet
      openstack.cloud.subnet:
        name: public
        network: public
        cidr: 10.30.0.0/24
        gateway_ip: 10.30.0.1
        enable_dhcp: false
        allocation_pool_start: 10.30.0.100
        allocation_pool_end: 10.30.0.199

    - name: Flavors
      openstack.cloud.compute_flavor:
        name: "{{ item.name }}"
        vcpus: "{{ item.vcpus }}"
        ram: "{{ item.ram }}"
        disk: "{{ item.disk }}"
      loop:
        - { name: s.tiny, vcpus: 1, ram: 256, disk: 1 }
        - { name: s.small, vcpus: 1, ram: 1024, disk: 10 }

    - name: Download CirrOS
      ansible.builtin.get_url:
        url: "https://download.cirros-cloud.net/{{ cirros_version }}/cirros-{{ cirros_version }}-x86_64-disk.img"
        dest: "{{ cirros_file }}"
        mode: "0644"

    - name: CirrOS image
      openstack.cloud.image:
        name: cirros
        filename: "{{ cirros_file }}"
        disk_format: qcow2
        container_format: bare
        is_public: true

    - name: Dev project
      openstack.cloud.project:
        name: dev
        domain: default

    - name: Backend service user
      openstack.cloud.identity_user:
        name: aleph0-api
        password: "{{ api_password }}"
        domain: default
        default_project: dev
        update_password: on_create

    - name: Backend service user is a member of dev
      openstack.cloud.role_assignment:
        user: aleph0-api
        role: member
        project: dev

    - name: Dev private network
      openstack.cloud.network:
        name: dev-net
        project: dev

    - name: Dev private subnet
      openstack.cloud.subnet:
        name: dev-subnet
        network: dev-net
        project: dev
        cidr: 192.168.100.0/24

    - name: Dev router to the external network
      openstack.cloud.router:
        name: dev-router
        project: dev
        network: public
        interfaces:
          - dev-subnet

    - name: Allow ICMP and SSH in dev's default security group
      openstack.cloud.security_group_rule:
        security_group: default
        project: dev
        direction: ingress
        protocol: "{{ item.protocol }}"
        port_range_min: "{{ item.port | default(omit) }}"
        port_range_max: "{{ item.port | default(omit) }}"
        remote_ip_prefix: 0.0.0.0/0
      loop:
        - { protocol: icmp }
        - { protocol: tcp, port: 22 }
```

- [ ] **Step 3: Run it from `apply`**

In `deploy/infra.sh`, inside `apply`, move `play markers.yml --limit "$PENDING"` out of the `if [[ -n $PENDING ]]` block and run the init before it, so the end of `apply` reads:

```bash
    if [[ ! -f $cluster_dir/clouds.yaml ]]; then
      ka post-deploy
    fi
  fi
  play openstack-init.yml
  if [[ -n $PENDING ]]; then
    play markers.yml --limit "$PENDING"
  fi
}
```

- [ ] **Step 4: Run it twice**

Run: `make infra-apply && make infra-apply`
Expected: the first run creates the resources; the second run's `openstack-init.yml` recap shows `changed=0` (the `get_url` task may report `ok`), `failed=0`. If the modules fail with an authentication or "no cloud" error, they are not picking up `OS_CLOUD`: add `cloud: kolla-admin` to each `openstack.cloud.*` task.

- [ ] **Step 5: Acceptance 1–3: a CirrOS server answers ping through a floating IP**

```bash
export OS_CLIENT_CONFIG_FILE=$PWD/deploy/kolla/dev/clouds.yaml OS_CLOUD=kolla-admin
devos() { deploy/.venv/bin/openstack --os-username aleph0-api --os-password "$(cat deploy/kolla/dev/credentials/aleph0-api)" --os-project-name dev "$@"; }
devos server create --flavor s.tiny --image cirros --network dev-net --wait test1
FIP=$(devos floating ip create public -f value -c floating_ip_address)
devos server add floating ip test1 "$FIP"
echo "$FIP" > /tmp/aleph0-fip
sleep 60
ping -c 3 "$FIP"
ssh ubuntu@10.10.0.11 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null 'sudo cephadm shell -- ceph -s | grep health'
```
Expected: `test1` reaches `ACTIVE`; `3 packets transmitted, 3 received`; `health: HEALTH_OK`. CirrOS boots slowly under `qemu`: if ping fails, wait another minute and retry before debugging (`devos console log show test1`).

- [ ] **Step 6: Commit**

```bash
git add deploy/playbooks/openstack-init.yml deploy/infra.sh
git commit -m "feat(deploy): create the initial OpenStack resources (#2)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Removing nodes (acceptance 4–6)

**Files:**
- Create: `deploy/playbooks/node-remove.yml`
- Modify: `deploy/infra.sh`, `Makefile` (append)

**Interfaces:**
- Consumes: `play`, `ka`, `tf`, `has_vms` (Tasks 3–4); the orphan check in `validate.yml` (Task 2); Terraform resource addresses (Task 3).
- Produces: `deploy/infra.sh remove <env> <node>`; `make node-remove ENV=<env> NODE=<name>`; `node-remove.yml` with tags `check` and `cleanup`, extra var `node`.

- [ ] **Step 1: Write `deploy/playbooks/node-remove.yml`**

```yaml
# Takes a compute and/or storage node out of the cluster. infra.sh runs tag `check`, then
# `kolla-ansible stop` on the node, then tag `cleanup`. Extra var: node=<name>.
- name: Check that the node can go, and stop scheduling on it
  hosts: localhost
  gather_facts: false
  tags: [check]
  vars:
    removing: "{{ hostvars[node].node_roles | default([]) }}"
    computes_left: "{{ groups['role_compute'] | default([]) | difference([node]) }}"
    storage_left: "{{ groups['role_storage'] | default([]) | difference([node]) }}"
  tasks:
    - name: The node is in the file and has only compute/storage roles
      ansible.builtin.assert:
        that:
          - node in groups['all']
          - removing | length > 0
          - removing | difference(['compute', 'storage']) | length == 0
        fail_msg: "{{ node }}: not in the node file, or has control/network roles (see deploy/README.md to remove those by hand)"

    - name: Another compute node remains
      ansible.builtin.assert:
        that: computes_left | length > 0
        fail_msg: "{{ node }} is the last compute node"
      when: "'compute' in removing"

    - name: Enough storage hosts remain for ceph_pool_size
      ansible.builtin.assert:
        that: storage_left | length >= ceph_pool_size | int
        fail_msg: "only {{ storage_left | length }} storage hosts would remain for ceph_pool_size {{ ceph_pool_size }}; lower ceph_pool_size first"
      when: "'storage' in removing"

    - name: Read Ceph health
      ansible.builtin.command: cephadm shell -- ceph health
      delegate_to: "{{ groups['role_control'][0] }}"
      become: true
      register: health
      changed_when: false

    - name: Ceph is healthy
      ansible.builtin.assert:
        that: health.stdout is search('^HEALTH_OK')
        fail_msg: "Ceph is not HEALTH_OK: {{ health.stdout }}"

    - name: Compute
      when: "'compute' in removing"
      block:
        - name: Stop scheduling new servers on the node
          ansible.builtin.command: openstack compute service set --disable {{ node }} nova-compute
          changed_when: true

        - name: List servers still on the node
          ansible.builtin.command: openstack server list --all-projects --host {{ node }} -f value -c ID
          register: servers
          changed_when: false

        - name: No servers are left on the node
          ansible.builtin.assert:
            that: servers.stdout_lines | length == 0
            fail_msg: |
              {{ node }} still runs servers and stays disabled. Move them, then rerun make node-remove:
              openstack server list --all-projects --host {{ node }} -f value -c ID | xargs -r -n1 openstack server migrate --live-migration --wait

- name: Remove the node from OpenStack and Ceph
  hosts: localhost
  gather_facts: false
  tags: [cleanup]
  vars:
    removing: "{{ hostvars[node].node_roles }}"
  tasks:
    - name: Compute
      when: "'compute' in removing"
      block:
        - name: Delete the node's network agents
          ansible.builtin.shell:
            cmd: set -o pipefail; openstack network agent list --host {{ node }} -f value -c ID | xargs -r -n1 openstack network agent delete
            executable: /bin/bash
          changed_when: true

        - name: Delete the node's compute service
          ansible.builtin.shell:
            cmd: >-
              set -o pipefail;
              openstack compute service list --os-compute-api-version 2.53 --host {{ node }} -f value -c ID
              | xargs -r -n1 openstack compute service delete --os-compute-api-version 2.53
            executable: /bin/bash
          changed_when: true

    - name: Storage
      when: "'storage' in removing"
      delegate_to: "{{ groups['role_control'][0] }}"
      become: true
      block:
        - name: Drain the node's Ceph daemons and data
          ansible.builtin.command: cephadm shell -- ceph orch host drain {{ node }}
          changed_when: true

        - name: Wait until no Ceph daemons are left on the node
          ansible.builtin.command: cephadm shell -- ceph orch ps {{ node }} -f json
          register: daemons
          changed_when: false
          retries: 90
          delay: 20
          until: daemons.stdout is search('No daemons') or (daemons.stdout | from_json | length == 0)

        - name: Remove the node from Ceph
          ansible.builtin.command: cephadm shell -- ceph orch host rm {{ node }}
          changed_when: true

    - name: Delete the node's marker
      ansible.builtin.file:
        path: /etc/aleph0/roles
        state: absent
      delegate_to: "{{ node }}"
      become: true
      ignore_unreachable: true
```

- [ ] **Step 2: Add `remove` to `deploy/infra.sh`**

After the `apply` function add:

```bash
remove() {
  local node=${1:?usage: infra.sh remove <env> <node>}
  play node-remove.yml --tags check -e "node=$node"
  ka stop --yes-i-really-really-mean-it --ignore-missing --limit "$node"
  play node-remove.yml --tags cleanup -e "node=$node"
  if has_vms; then
    tf workspace select "$env"
    tf destroy -input=false -auto-approve -var "nodes_file=$PWD/$nodes" \
      -target="libvirt_domain.node[\"$node\"]" \
      -target="libvirt_volume.system[\"$node\"]" \
      -target="libvirt_volume.ceph[\"$node\"]" \
      -target="libvirt_volume.init[\"$node\"]" \
      -target="libvirt_cloudinit_disk.init[\"$node\"]"
  fi
  echo "Remove $node from deploy/nodes/$env.yml."
}
```

and add the command to the `case`:

```bash
  remove) remove "${3:-}" ;;
```

- [ ] **Step 3: Add the Make target**

Append to `Makefile`:

```make

.PHONY: node-remove
node-remove: deploy/.venv/.done
	deploy/infra.sh remove $(ENV) $(NODE)
```

- [ ] **Step 4: Acceptance 4: add node4 while test1 keeps answering**

Add to the `hosts:` of `deploy/nodes/dev.yml` (not committed):

```yaml
    node4:
      ansible_host: 10.10.0.14
      storage_ip: 10.20.0.14
      node_roles: [compute, storage]
      vm: { vcpus: 4, memory_gb: 4, ceph_disk_gb: 50 }
```

```bash
FIP=$(cat /tmp/aleph0-fip)
ping -i 1 "$FIP" > /tmp/aleph0-ping.log 2>&1 &
PING_PID=$!
make infra-apply
cat deploy/kolla/dev/pending.env
kill -INT "$PING_PID"; sleep 1; tail -2 /tmp/aleph0-ping.log
```
Expected: `PENDING=node4`, `PENDING_CONTROL=no`, and the apply log shows `deploy --limit node4`; the ping summary shows `0% packet loss` (report any lost packets).

Then place a server on node4:

```bash
export OS_CLIENT_CONFIG_FILE=$PWD/deploy/kolla/dev/clouds.yaml OS_CLOUD=kolla-admin
devos() { deploy/.venv/bin/openstack --os-username aleph0-api --os-password "$(cat deploy/kolla/dev/credentials/aleph0-api)" --os-project-name dev "$@"; }
for n in node1 node2 node3; do deploy/.venv/bin/openstack compute service set --disable $n nova-compute; done
devos server create --flavor s.tiny --image cirros --network dev-net --wait test2
deploy/.venv/bin/openstack server show "$(devos server show test2 -f value -c id)" -c OS-EXT-SRV-ATTR:host -f value
for n in node1 node2 node3; do deploy/.venv/bin/openstack compute service set --enable $n nova-compute; done
ssh ubuntu@10.10.0.11 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null 'sudo cephadm shell -- ceph osd tree'
```
Expected: `test2` ACTIVE on `node4`; `ceph osd tree` lists 4 hosts with one OSD each, all `up`.

- [ ] **Step 5: Acceptance 6: deleting node4 from the file is refused**

Remove the `node4` block from `deploy/nodes/dev.yml`, then:

Run: `make infra-apply`
Expected: FAIL in `validate.yml` with `node4: still part of the cluster: run make node-remove ENV=dev NODE=<name>, or put it back in the file`; `virsh -c qemu:///system list | grep aleph0-node4` still shows the VM running. Put the `node4` block back.

- [ ] **Step 6: Acceptance 5: remove node4**

```bash
make node-remove NODE=node4
```
Expected: FAIL at `No servers are left on the node`, printing the migrate command (test2 is still there).

```bash
devos server delete --wait test2
make node-remove NODE=node4
```
Expected: completes and prints `Remove node4 from deploy/nodes/dev.yml.` Then:

```bash
git checkout deploy/nodes/dev.yml
virsh -c qemu:///system list --all | grep aleph0-node4 || echo "node4 gone"
deploy/.venv/bin/openstack compute service list -c Host -f value | sort -u
ssh ubuntu@10.10.0.11 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null 'sudo cephadm shell -- ceph -s | grep -E "health|osd:"'
ping -c 3 "$(cat /tmp/aleph0-fip)"
make infra-apply
```
Expected: `node4 gone`; hosts node1–node3 only; `HEALTH_OK` with 3 OSDs; ping answers; the final `infra-apply` passes the orphan check and has nothing pending.

- [ ] **Step 7: Commit**

```bash
git add deploy/playbooks/node-remove.yml deploy/infra.sh Makefile
git commit -m "feat(deploy): remove compute and storage nodes from a running cluster (#2)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Docs and the clean run (acceptance 1–3, 7)

**Files:**
- Create: `deploy/README.md`
- Modify: `docs/architecture.md`, `README.md` (one link line, only if it has a section listing docs)

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Write `deploy/README.md`**

````markdown
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
| `make infra-destroy` | Deletes the dev VMs and the generated files (dev only) |
| `make infra-test` | Checks the validation rules and the Terraform config, no cluster needed |

The first `infra-apply` takes about an hour; later runs only deploy the nodes that changed.

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

## Adding a node

Add an entry to the node file and run `make infra-apply`. Only the new node is deployed, unless
it has the `control` role: then Kolla runs on every node (APIs may blip; tenant VMs keep
running). Controllers go from 1 to 3 in one apply.

## Removing a node

- Compute/storage: move the servers off it, run `make node-remove NODE=<name>`, then delete its
  entry from the node file. Deleting the entry first makes `infra-apply` refuse to run.
- Control or network (manual, for now; Kolla's "Removing existing controllers" order). Keep
  an odd number of controllers, and remove one at a time. With
  `KA="deploy/.venv/bin/kolla-ansible -i deploy/nodes/dev.yml -i deploy/nodes/roles.yml -i deploy/kolla/services.ini --configdir $PWD/deploy/kolla/dev --passwords $PWD/deploy/kolla/dev/passwords.yml"`:
  1. Stop its services: `$KA stop --yes-i-really-really-mean-it --limit <node>`.
  2. Delete its entry from the node file.
  3. Controller: `$KA deploy --limit control`, so MariaDB and RabbitMQ drop the old member.
     Network: OVN moves the routers' gateways to the remaining network nodes.
  4. Check the cluster: `openstack compute service list`, `openstack network agent list`.
  5. Delete its network agents and compute service, as in `deploy/playbooks/node-remove.yml`.
  6. If it is a Ceph host: `ceph orch host drain <node>`, wait until `ceph orch ps <node>` lists
     no daemons, then `ceph orch host rm <node>`.
  7. Delete `/etc/aleph0/roles` on the node.

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

`deploy/kolla/<env>/` holds `passwords.yml`, `clouds.yaml`, `credentials/`, the Ceph keys in
`config/` and `pending.env`. They are gitignored; only `globals.yml` is committed.
Each node records its deployed roles in `/etc/aleph0/roles`.
````

- [ ] **Step 2: Update `docs/architecture.md`**

Make these replacements (exact old text → new text):

1. In the §2 diagram, `│ Own infrastructure — 3 nodes                   │` → `│ Own infrastructure — N nodes (3 at launch)     │`.

2. In §3, replace the paragraph `Three VMs, running on libvirt/KVM inside WSL2, mimic the three physical nodes. Same Kolla-Ansible`
   `configuration as production; only IPs, disks and the HA level differ.` with:

   ```markdown
   The cluster is described by a node file (`deploy/nodes/<env>.yml`): each node has roles
   (`control`, `network`, `compute`, `storage`), and nodes can be added or removed while the
   cluster runs. In dev, nodes are libvirt/KVM VMs inside WSL2; in production, the same scripts
   run against physical machines. See `deploy/README.md`.
   ```

3. In §3, replace the whole `### Dev VMs` subsection (heading, the sentence, the table and its four bullets) with:

   ```markdown
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
   - Horizon is disabled (we have our own portal); Octavia is enabled with the load balancer step
   ```

4. In the dev vs production table, `| Control nodes | 1 (no HA) | 3 (HA: HAProxy + keepalived, Galera, RabbitMQ cluster) |` → `| Control nodes | 1, behind a VIP (HAProxy + keepalived) | 3 or more, odd (HA: HAProxy + keepalived, Galera, RabbitMQ cluster) |`.

5. In §4, `| Kolla-Ansible + Ansible | installing OpenStack |` → `| Kolla-Ansible + Ansible | installing OpenStack; our playbooks prepare hosts, Ceph and the initial resources |`, and `| cephadm | installing Ceph |` stays.

6. In §5, replace the `deploy/` part of the tree:

   ```
   ├── deploy/
   │   ├── postgres/            # local DB init
   │   ├── keycloak/            # exported realm
   │   ├── terraform/           # dev VMs
   │   ├── ceph/
   │   └── kolla/{dev,prod}/    # inventory + globals.yml
   ```
   with:
   ```
   ├── deploy/
   │   ├── postgres/            # local DB init
   │   ├── keycloak/            # exported realm
   │   ├── infra.sh             # apply | remove | destroy | test
   │   ├── nodes/               # node files (one per env) + role groups
   │   ├── playbooks/           # hosts, Ceph, OpenStack init, node removal
   │   ├── terraform/           # dev VMs from the node file
   │   └── kolla/               # services.ini + <env>/globals.yml
   ```

7. Replace §6 from `### 6.1 VMs` up to (not including) `## 7. Database (minimum)` with:

   ````markdown
   ### 6.1 Infrastructure

   ```bash
   make infra-apply      # VMs, Ceph, OpenStack and the initial resources, from deploy/nodes/dev.yml
   make infra-destroy    # wipe the dev cluster
   ```

   Requirements, adding and removing nodes: `deploy/README.md`. Main Kolla settings
   (`deploy/kolla/dev/globals.yml`):

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
   ```

   ### 6.2 Initial OpenStack setup

   Created by `make infra-apply`: external network `public` with floating IPs, flavors `s.tiny`
   and `s.small`, CirrOS, project `dev` with a network and router, and the backend service user
   `aleph0-api` (`member` on `dev`). Kolla's `admin` is the local emergency admin (not federated).
   Later: Ubuntu and Debian images, default per-project quotas.
   ````

8. In the `## 6. Getting started (dev)` intro, replace the two lines

   ```
   The app stack (portal, API, Keycloak, PostgreSQL) is described in `README.md`. The infrastructure
   steps below belong to issue #2.
   ```
   with
   ```
   The app stack (portal, API, Keycloak, PostgreSQL) is described in `README.md`, the
   infrastructure in `deploy/README.md`.
   ```

9. Replace `## 9. Production (3 physical nodes) — notes` and its bullets with:

   ```markdown
   ## 9. Production notes

   Starts with 3 physical nodes, each with all four roles; more nodes are added to the node file.

   - HA control plane on 3 controllers (odd count); the cluster tolerates losing **one** of them
   - Ceph with `ceph_pool_size: 3` (one copy per host)
   - Max load ~(N−1)/N of capacity, so a failed node's VMs fit on the others
   - Server groups with **anti-affinity** for customers who want redundancy
   - VM HA: evacuation driven by the backend, or **Masakari** (later)
   ```

- [ ] **Step 3: Check the docs for leftovers**

```bash
grep -n -i "3 nodes\|three nodes\|three VMs\|snapshot the 3\|node1's IP\|enable_haproxy: \"no\"" docs/architecture.md deploy/README.md || echo "clean"
```
Expected: `clean`.

- [ ] **Step 4: Acceptance 1–3 and 7 from a clean state**

```bash
make infra-destroy
virsh -c qemu:///system list --all | grep aleph0 || echo "no aleph0 domains"   # acceptance 7
make infra-test
time make infra-apply                                                           # acceptance 1
```
Then repeat Task 7 Step 5 (acceptance 2–3), and finish with:

```bash
make infra-destroy
virsh -c qemu:///system list --all | grep aleph0 || echo "no aleph0 domains"
ls deploy/kolla/dev
```
Expected: `no aleph0 domains` both times; `infra-apply` completes with no manual step; test1 answers ping and Ceph is `HEALTH_OK`; `deploy/kolla/dev` contains only `globals.yml`. (Leave the cluster up instead of the final destroy if #4 starts next.)

- [ ] **Step 5: Commit**

```bash
git add deploy/README.md docs/architecture.md
git commit -m "docs: describe the node-file infrastructure (#2)" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
