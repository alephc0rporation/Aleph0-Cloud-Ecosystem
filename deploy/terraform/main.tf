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
  target   = { format = { type = "iso" } }
  create   = { content = { url = libvirt_cloudinit_disk.init[each.key].path } }
  # The provider keeps the generated ISO in /tmp, which a reboot empties, and regenerates it under
  # a new path: replace the volume when the cloud-init content changes, not when the path does.
  lifecycle {
    ignore_changes       = [create]
    replace_triggered_by = [terraform_data.init_content[each.key]]
  }
}

resource "terraform_data" "init_content" {
  for_each = local.vms
  input = {
    meta_data      = libvirt_cloudinit_disk.init[each.key].meta_data
    user_data      = libvirt_cloudinit_disk.init[each.key].user_data
    network_config = libvirt_cloudinit_disk.init[each.key].network_config
  }
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
  # Without this libvirt starts QEMU with acpi=off, and the guest never comes up on the network.
  features = { acpi = true, apic = {} }

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
