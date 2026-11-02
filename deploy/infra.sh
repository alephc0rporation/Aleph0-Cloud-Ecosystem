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
# The aleph0 VMs (running ones; --inactive for the stopped ones). Assign it (v=$(aleph0_vms)) so
# that an unreachable libvirt stops the script; no aleph0 VM at all is a normal, empty answer.
aleph0_vms() {
  local names
  names=$(virsh list --name "$@") || { echo "virsh cannot reach libvirt (qemu:///system); is libvirtd running?" >&2; exit 1; }
  grep '^aleph0-' <<<"$names" || true
}
# Against stopped dev VMs, apply, remove and reconfigure would fail later with raw SSH errors.
vms_up() {
  local stopped
  if [[ $env == dev ]] && has_vms; then
    stopped=$(aleph0_vms --inactive)
    if [[ -n $stopped ]]; then
      echo "dev VMs are stopped: run make infra-start first" >&2
      exit 1
    fi
  fi
}
# The timeout keeps a call short while the monitors are not in quorum, so callers can retry.
ceph_admin() { ansible "${inv[@]}" 'role_control[0]' -b -m command -a "cephadm shell -- ceph --connect-timeout 30 $*"; }
api_up() {
  local _
  for _ in $(seq 30); do
    openstack token issue -f value -c id >/dev/null 2>&1 && return 0
    sleep 10
  done
  return 1
}
ka() { kolla-ansible "$1" "${inv[@]}" --configdir "$cluster_dir" --passwords "$cluster_dir/passwords.yml" "${@:2}"; }
# Reads `terraform show -json <plan>` on stdin; fails, listing the refused resources and then $2,
# if the plan does more than allowed. With a node ($1): anything but deleting that node's
# resources (node-remove). Without one: any delete, which a replace includes (infra-apply).
tf_check() {
  python3 -c '
import json, sys
node, key = sys.argv[1], "[\"" + sys.argv[1] + "\"]"
def refused(actions, address):
    if not node:
        return "delete" in actions
    return actions not in (["no-op"], ["read"]) and not (actions == ["delete"] and key in address)
bad = [c["address"] for c in json.load(sys.stdin).get("resource_changes", [])
       if refused(c["change"]["actions"], c["address"])]
if bad:
    sys.exit("\n".join(bad) + "\n" + sys.argv[2])
' "$1" "$2"
}

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
  if [[ ! -f $cluster_dir/clouds.yaml ]]; then
    tf apply -input=false -auto-approve -var "nodes_file=$PWD/$nodes"
    return
  fi
  # A cluster exists: new machines and in-place updates only, never a delete or replace.
  local plan=$cluster_dir/apply.tfplan
  trap "rm -f '$plan'" EXIT
  tf plan -input=false -var "nodes_file=$PWD/$nodes" -out="$plan"
  tf show -json "$plan" | tf_check "" "Terraform would delete or replace the resources above, which infra-apply never does on a running cluster. Undo that change in deploy/$nodes; to rebuild a node, make node-remove it, then add it back."
  tf apply -input=false "$plan"
}

apply() {
  vms_up
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
      if [[ $attempt == 3 ]]; then
        echo "kolla-ansible pull failed after 3 attempts; rerun make infra-apply" >&2
        exit 1
      fi
      sleep 30
    done
    if [[ $PENDING_CONTROL == yes ]]; then
      ka deploy   # HAProxy, Galera, RabbitMQ and the OVN databases must learn the new controller
    else
      ka deploy --limit "$limit"
    fi
    if [[ ! -f $cluster_dir/clouds.yaml ]]; then
      ka post-deploy
    fi
    # Before the init, which runs on every apply: a failed init must not redeploy the nodes.
    play markers.yml --limit "$PENDING"
  fi
  play openstack-init.yml
}

remove() {
  local node=${1:?usage: infra.sh remove <env> <node>}
  local rest plan=$cluster_dir/remove-$node.tfplan skipped=
  vms_up
  if has_vms; then
    # Plan the Terraform change first, before anything touches the node, and refuse unless it only
    # deletes resources of this node. Apply the node file without the node: a targeted destroy would
    # take every VM with it, because all domains depend on the same volume resources.
    rest=$(mktemp --suffix=.yml)
    trap "rm -f '$rest' '$plan'" EXIT
    python3 -c 'import sys, yaml; d = yaml.safe_load(open(sys.argv[1])); d["all"]["hosts"].pop(sys.argv[2], None); yaml.safe_dump(d, open(sys.argv[3], "w"))' "$nodes" "$node" "$rest"
    tf workspace select "$env"
    tf plan -input=false -var "nodes_file=$rest" -out="$plan"
    tf show -json "$plan" | tf_check "$node" "Terraform would change more than $node; run make infra-apply first and retry"
  fi
  play node-remove.yml --tags check -e "node=$node"
  # destroy (not stop): a physical node that reboots must not bring its OpenStack containers back.
  # A rerun after an interruption may find the node already off or gone.
  if ansible "$node" "${inv[@]}" -m ansible.builtin.ping >/dev/null 2>&1; then
    ka destroy --yes-i-really-really-mean-it --limit "$node"
  else
    skipped=yes
    echo "$node does not answer: skipping the Kolla cleanup on it. A physical machine may rejoin after a reboot: keep $node in the node file until kolla-ansible destroy --limit $node has run on it (see deploy/README.md)."
  fi
  play node-remove.yml --tags cleanup -e "node=$node"
  if has_vms; then
    tf apply -input=false "$plan"   # nothing in between touches Terraform state, so the plan is still valid
  fi
  if [[ $skipped == yes ]] && ! has_vms; then   # a dev VM is deleted with its node: nothing to clean up later
    echo "$node is out of Ceph and OpenStack. Keep it in deploy/nodes/$env.yml until its Kolla cleanup has run (deploy/README.md), and do not run infra-apply meanwhile; then remove it from the file."
  else
    echo "Remove $node from deploy/nodes/$env.yml."
  fi
}

destroy() {
  dev_only infra-destroy
  local answer
  if [[ -t 0 ]]; then   # scripts and CI, without a terminal, are not asked
    read -r -p "Destroy the dev cluster? [y/N] " answer
    if [[ $answer != [yY] ]]; then
      echo "Nothing destroyed."
      exit 1
    fi
  fi
  tf init -input=false >/dev/null
  tf workspace select -or-create dev
  tf destroy -input=false -auto-approve -var "nodes_file=$PWD/$nodes"
  find "$cluster_dir" -mindepth 1 -maxdepth 1 ! -name globals.yml -exec rm -rf {} +
}

stop() {
  dev_only infra-stop
  local d _ running
  running=$(aleph0_vms)
  if [[ -z $running ]]; then
    echo "All aleph0 VMs are off."
    return
  fi
  # Every node goes down at once: Ceph must not start re-replicating data meanwhile.
  ceph_admin osd set noout || echo "Ceph did not answer; stopping anyway." >&2
  for d in $running; do virsh shutdown "$d" >/dev/null; done
  for _ in $(seq 60); do
    running=$(aleph0_vms)
    if [[ -z $running ]]; then
      echo "All aleph0 VMs are off."
      return
    fi
    sleep 5
  done
  echo "VMs still running after 5 minutes; forcing them off: $(xargs <<<"$running")" >&2
  for d in $running; do virsh destroy "$d" >/dev/null || true; done   # one may finish its shutdown meanwhile
  running=$(aleph0_vms)
  if [[ -n $running ]]; then
    echo "Could not force off: $(xargs <<<"$running")" >&2
    exit 1
  fi
  echo "VMs were forced off; make infra-start runs mariadb-recovery itself if the OpenStack API does not come back."
}

start() {
  dev_only infra-start
  local d stopped deadline noout=set health=
  stopped=$(aleph0_vms --inactive)
  for d in $stopped; do virsh start "$d" >/dev/null; done
  play hosts.yml
  # Right after boot the monitors may not be in quorum yet. Unsetting noout is idempotent: retry it.
  deadline=$((SECONDS + 600))
  while (( SECONDS < deadline )); do
    if [[ $noout == set ]] && ceph_admin osd unset noout >/dev/null 2>&1; then noout=unset; fi
    if [[ $noout == unset ]]; then
      health=$(ceph_admin health 2>&1) || true
      [[ $health == *HEALTH_OK* ]] && break
    fi
    sleep 10
  done
  if [[ $noout == set ]]; then
    echo "Ceph is not up yet; rerun make infra-start" >&2
    exit 1
  fi
  if [[ $health == *HEALTH_OK* ]]; then
    echo "Ceph is HEALTH_OK."
  else
    echo "Warning: Ceph is not HEALTH_OK after 10 minutes; going on. $(sed -n 's/Inferring fsid.*//; /^HEALTH_/p' <<<"$health")" >&2
  fi
  # start.yml restarts the containers that came up before MariaDB and checks every service.
  if [[ -f $cluster_dir/clouds.yaml ]] && ! play start.yml; then
    if api_up; then
      echo "The OpenStack API answers, so MariaDB is fine; fix the failed task above, then rerun make infra-start" >&2
      exit 1
    fi
    echo "OpenStack API is not answering; recovering MariaDB." >&2
    ka mariadb-recovery
    if ! play start.yml; then
      echo "OpenStack API still not answering after mariadb-recovery; see the failed task above" >&2
      exit 1
    fi
  fi
  echo "Cluster is up. Servers inside OpenStack come back SHUTOFF: openstack server start <name>."
}

# Each bad fixture must be rejected by its own rule: <fixture>:<text of that rule's message>.
run_tests() {
  ansible-playbook -i nodes/testdata/valid.yml -i nodes/roles.yml playbooks/validate.yml
  local t f msg out
  # With cluster_dir set, the checks against an existing cluster run; a first apply has no
  # cluster yet: no controller answers and there is no clouds.yaml.
  if ! out=$(ANSIBLE_TIMEOUT=5 ansible-playbook -i nodes/testdata/fresh-cluster.yml -i nodes/roles.yml \
               -e cluster_dir="$PWD/nodes/testdata/no-cluster" playbooks/validate.yml 2>&1); then
    echo "FAIL: nodes/testdata/fresh-cluster.yml failed validation before the first apply:" >&2
    echo "$out" >&2
    exit 1
  fi
  echo "ok: nodes/testdata/fresh-cluster.yml passes before the first apply"
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
  # tf_check on canned plans, per mode (apply, or removing node3) one allowed and one refused.
  # Each case: <expected result>|<node>|<resource changes>.
  local want node changes got
  local create='{"address": "libvirt_domain.node[\"node4\"]", "change": {"actions": ["create"]}}'
  local replace='{"address": "libvirt_volume.ceph[\"node1\"]", "change": {"actions": ["delete", "create"]}}'
  local gone='{"address": "libvirt_volume.ceph[\"node3\"]", "change": {"actions": ["delete"]}}'
  for t in "allowed||$create" "refused libvirt_volume.ceph[\"node1\"]||$create, $replace" \
           "allowed|node3|$gone" "refused libvirt_domain.node[\"node4\"]|node3|$gone, $create"; do
    IFS='|' read -r want node changes <<<"$t"
    if out=$(tf_check "$node" "the refusal" <<<"{\"resource_changes\": [$changes]}" 2>&1); then got=allowed; else got="refused $out"; fi
    if [[ $got != "$want"* ]]; then
      echo "FAIL: tf_check ${node:-apply} on [$changes] gave: $got" >&2
      exit 1
    fi
    echo "ok: tf_check ${node:-apply}: $want"
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
  reconfigure) vms_up; ka reconfigure ${3:+--tags "$3"} ;;
  test) run_tests ;;
  *) echo "unknown command: $cmd" >&2; exit 2 ;;
esac
