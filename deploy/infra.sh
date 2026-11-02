#!/usr/bin/env bash
# Builds and changes the OpenStack + Ceph cluster described by deploy/nodes/<env>.yml.
# Usage: infra.sh apply|remove|destroy|stop|start|reconfigure|test <env> [node|tags]
set -euo pipefail
cd "$(dirname "$0")"

cmd=${1:?usage: infra.sh apply|remove|destroy|stop|start|reconfigure|test <env> [node|tags]}
env=${2:-dev}
nodes=nodes/$env.yml
cluster_dir=$PWD/kolla/$env
if [[ $cmd != test && ! -f $nodes ]]; then echo "no such node file: deploy/$nodes" >&2; exit 2; fi
mkdir -p "$cluster_dir" && chmod 700 "$cluster_dir"   # holds keyrings, passwords and credentials

export PATH=$PWD/.venv/bin:$PATH
export OS_CLIENT_CONFIG_FILE=$cluster_dir/clouds.yaml OS_CLOUD=kolla-admin
if [[ $env == dev ]]; then
  # Dev VMs are recreated with the same addresses, so their host keys change.
  export ANSIBLE_HOST_KEY_CHECKING=False
  export ANSIBLE_SSH_ARGS="-o ControlMaster=auto -o ControlPersist=60s -o UserKnownHostsFile=/dev/null"
fi

inv=(-i "$nodes" -i nodes/roles.yml -i kolla/services.ini)

play() { ansible-playbook "${inv[@]}" -e "cluster_env=$env cluster_dir=$cluster_dir" "playbooks/$1" "${@:2}"; }
tf() { (cd terraform && terraform "$@"); }
has_vms() { grep -q '^ *vm:' "$nodes"; }
dev_only() {
  if [[ $env != dev ]]; then
    echo "$1 only runs for dev" >&2
    exit 1
  fi
}
virsh() { command virsh -c qemu:///system "$@"; }
vms_running() { virsh list --name | grep '^aleph0-' || true; }
ceph_admin() { ansible "${inv[@]}" 'role_control[0]' -b -m command -a "cephadm shell -- ceph $*"; }
api_up() {
  local _
  for _ in $(seq 30); do
    openstack token issue -f value -c id >/dev/null 2>&1 && return 0
    sleep 10
  done
  return 1
}
ka() { kolla-ansible "$1" "${inv[@]}" --configdir "$cluster_dir" --passwords "$cluster_dir/passwords.yml" "${@:2}"; }

passwords() {
  if [[ ! -f $cluster_dir/passwords.yml ]]; then
    cp .venv/share/kolla-ansible/etc_examples/kolla/passwords.yml "$cluster_dir/passwords.yml"
    kolla-genpwd -p "$cluster_dir/passwords.yml"
  fi
}

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
  play ceph.yml   # every run, so ceph_pool_size and spec changes apply even when no node changed
  if [[ -n $PENDING ]]; then
    ka prechecks --limit "$limit"
    for attempt in 1 2 3; do   # quay.io resets connections mid-download from the VMs
      echo "kolla-ansible pull, attempt $attempt of 3"
      ka pull --limit "$limit" && break
      [[ $attempt == 3 ]] && exit 1
    done
    if [[ $PENDING_CONTROL == yes ]]; then
      ka deploy   # HAProxy, Galera, RabbitMQ and the OVN databases must learn the new controller
    else
      ka deploy --limit "$limit"
    fi
    if [[ ! -f $cluster_dir/clouds.yaml ]]; then
      ka post-deploy
    fi
  fi
  play openstack-init.yml
  if [[ -n $PENDING ]]; then
    play markers.yml --limit "$PENDING"
  fi
}

remove() {
  local node=${1:?usage: infra.sh remove <env> <node>}
  local rest plan=$cluster_dir/remove-$node.tfplan
  if has_vms; then
    # Plan the Terraform change first, before anything touches the node, and refuse unless it only
    # deletes resources of this node. Apply the node file without the node: a targeted destroy would
    # take every VM with it, because all domains depend on the same volume resources.
    rest=$(mktemp --suffix=.yml)
    trap "rm -f '$rest' '$plan'" EXIT
    python3 -c 'import sys, yaml; d = yaml.safe_load(open(sys.argv[1])); d["all"]["hosts"].pop(sys.argv[2], None); yaml.safe_dump(d, open(sys.argv[3], "w"))' "$nodes" "$node" "$rest"
    tf workspace select "$env"
    tf plan -input=false -var "nodes_file=$rest" -out="$plan"
    tf show -json "$plan" | python3 -c '
import json, sys
node, key = sys.argv[1], "[\"" + sys.argv[1] + "\"]"
other = [c["address"] for c in json.load(sys.stdin).get("resource_changes", [])
         if c["change"]["actions"] not in (["no-op"], ["read"])
         and not (c["change"]["actions"] == ["delete"] and key in c["address"])]
if other:
    sys.exit("\n".join(other) + "\nTerraform would change more than " + node + "; run make infra-apply first and retry")
' "$node"
  fi
  play node-remove.yml --tags check -e "node=$node"
  # destroy (not stop): a physical node that reboots must not bring its OpenStack containers back.
  # A rerun after an interruption may find the node already off or gone.
  if ansible "$node" "${inv[@]}" -m ansible.builtin.ping >/dev/null 2>&1; then
    ka destroy --yes-i-really-really-mean-it --limit "$node"
  else
    echo "$node does not answer: skipping the Kolla cleanup on it; run kolla-ansible destroy --limit $node on it once it is reachable, or it may rejoin after a reboot."
  fi
  play node-remove.yml --tags cleanup -e "node=$node"
  if has_vms; then
    tf apply -input=false "$plan"   # nothing in between touches Terraform state, so the plan is still valid
  fi
  echo "Remove $node from deploy/nodes/$env.yml."
}

destroy() {
  dev_only infra-destroy
  tf init -input=false >/dev/null
  tf workspace select -or-create dev
  tf destroy -input=false -auto-approve -var "nodes_file=$PWD/$nodes"
  find "$cluster_dir" -mindepth 1 -maxdepth 1 ! -name globals.yml -exec rm -rf {} +
}

stop() {
  dev_only infra-stop
  # Every node goes down at once: Ceph must not start re-replicating data meanwhile.
  ceph_admin osd set noout || echo "Ceph did not answer; stopping anyway." >&2
  local d _
  for d in $(vms_running); do virsh shutdown "$d" >/dev/null; done
  for _ in $(seq 60); do
    if [[ -z $(vms_running) ]]; then
      echo "All aleph0 VMs are off."
      return
    fi
    sleep 5
  done
  echo "VMs still running after 5 minutes; forcing them off: $(vms_running | xargs)" >&2
  for d in $(vms_running); do virsh destroy "$d" >/dev/null; done
}

start() {
  dev_only infra-start
  local d _
  for d in $(virsh list --inactive --name | grep '^aleph0-' || true); do virsh start "$d" >/dev/null; done
  play hosts.yml
  ceph_admin osd unset noout
  for _ in $(seq 60); do
    [[ $(ceph_admin health) == *HEALTH_OK* ]] && break   # not grep -q: under pipefail its SIGPIPE fails the check
    sleep 10
  done
  ceph_admin health
  # start.yml restarts the containers that came up before MariaDB and checks every service.
  if [[ -f $cluster_dir/clouds.yaml ]] && ! play start.yml; then
    if api_up; then
      echo "The OpenStack API answers, so MariaDB is fine; see the failed task above." >&2
      exit 1
    fi
    echo "OpenStack API is not answering; recovering MariaDB." >&2
    ka mariadb-recovery
    play start.yml
  fi
  echo "Cluster is up. Servers inside OpenStack come back SHUTOFF: openstack server start <name>."
}

# Each bad fixture must be rejected by its own rule: <fixture>:<text of that rule's message>.
run_tests() {
  ansible-playbook -i nodes/testdata/valid.yml -i nodes/roles.yml playbooks/validate.yml
  local t f msg out
  for t in "two-controllers:use 1, 3 or 5" \
           "no-network:no node has the network role" \
           "too-few-storage:1 storage hosts for ceph_pool_size 2" \
           "unknown-role:needs ansible_host, storage_ip and node_roles" \
           "empty:the node file lists no nodes"; do
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
  remove) remove "${3:-}" ;;
  destroy) destroy ;;
  stop) stop ;;
  start) start ;;
  reconfigure) ka reconfigure ${3:+--tags "$3"} ;;
  test) run_tests ;;
  *) echo "unknown command: $cmd" >&2; exit 2 ;;
esac
