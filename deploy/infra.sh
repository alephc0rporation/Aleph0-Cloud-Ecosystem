#!/usr/bin/env bash
# Builds and changes the OpenStack + Ceph cluster described by deploy/nodes/<env>.yml.
# Usage: infra.sh apply|remove|destroy|test <env> [node]
set -euo pipefail
cd "$(dirname "$0")"

cmd=${1:?usage: infra.sh apply|remove|destroy|test <env> [node]}
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
    ka pull --limit "$limit"
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
  destroy) destroy ;;
  test) run_tests ;;
  *) echo "unknown command: $cmd" >&2; exit 2 ;;
esac
