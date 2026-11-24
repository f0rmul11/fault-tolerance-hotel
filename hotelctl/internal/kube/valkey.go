package kube

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const valkeyImage = "valkey/valkey:9.0.8-alpine@sha256:139d21c97b1a956e42e29b1e4ede4752a6ff52d1a0db9ef0ff4954a570f23976"

// Readiness узлов требует собранного кластера, поэтому bootstrap ждёт PING.
const bootstrapValkey = `set -eu
node() { printf 'valkey-%s.valkey-headless.hotelflow.svc.cluster.local' "$1"; }
v() { host=$1; shift; valkey-cli -e --raw -h "$host" "$@"; }
fresh=0
addresses=''
for i in 0 1 2 3 4 5; do
    host=$(node "$i")
    until [ "$(v "$host" ping 2>/dev/null || true)" = PONG ]; do sleep 2; done
    address=$(v "$host" client info | tr ' ' '\n' | sed -n 's/^laddr=//p')
    [ -n "$address" ]
    addresses="$addresses $address"
    info=$(v "$host" cluster info | tr -d '\r')
    if printf '%s\n' "$info" | grep -qx 'cluster_known_nodes:1' &&
       printf '%s\n' "$info" | grep -qx 'cluster_slots_assigned:0'; then
        [ "$(v "$host" dbsize)" = 0 ] || { echo 'Valkey: узел содержит данные, создание кластера отменено' >&2; exit 1; }
        fresh=$((fresh+1))
    fi
done
if [ "$fresh" = 6 ]; then
    valkey-cli -e --cluster create $addresses --cluster-replicas 1 --cluster-yes
elif [ "$fresh" != 0 ]; then
    echo 'Valkey: кластер создан частично; проверьте узлы и PVC' >&2
    exit 1
else
    for address in $addresses; do
        v "$(node 0)" cluster meet "${address%:*}" 6379 >/dev/null
    done
fi
healthy() {
    for i in 0 1 2 3 4 5; do
        info=$(v "$(node "$i")" cluster info | tr -d '\r') || return 1
        for expected in cluster_state:ok cluster_slots_assigned:16384 cluster_slots_ok:16384 cluster_slots_fail:0 cluster_slots_pfail:0 cluster_known_nodes:6 cluster_size:3; do
            printf '%s\n' "$info" | grep -qx "$expected" || return 1
        done
        replication=$(v "$(node "$i")" info replication | tr -d '\r') || return 1
        if printf '%s\n' "$replication" | grep -qx 'role:slave'; then
            printf '%s\n' "$replication" | grep -qx 'master_link_status:up' || return 1
            printf '%s\n' "$replication" | grep -qx 'master_sync_in_progress:0' || return 1
        fi
    done
    nodes=$(v "$(node 0)" cluster nodes) || return 1
    printf '%s\n' "$nodes" | awk '$3 ~ /master/ {m++} $3 ~ /slave/ {r++; replicas[$4]++} END {if(m!=3 || r!=3) exit 1; for(id in replicas) if(replicas[id]!=1) exit 1}'
}
until healthy; do sleep 2; done
echo 'Valkey готов: 3 primary, 3 replica, 16384 слота'
`

func valkeyInitContainer() corev1.Container {
	return corev1.Container{
		Name:    "valkey-cluster",
		Image:   valkeyImage,
		Command: []string{"/bin/sh", "-ec", bootstrapValkey},
	}
}

func (c *Client) WaitValkey(ctx context.Context) error {
	return Poll(ctx, "готовность Valkey", func(ctx context.Context) (bool, error) {
		state, err := c.Typed.AppsV1().StatefulSets(bookingNamespace).Get(ctx, "valkey", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return state.Status.ObservedGeneration >= state.Generation && state.Status.ReadyReplicas == 6, nil
	})
}
