package kube

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValkeyBootstrap(t *testing.T) {
	for _, scenario := range []string{"fresh", "existing", "partial", "nonempty"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			cli := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$TEST_DIR/calls"
if [ "$2" = --cluster ]; then touch "$TEST_DIR/created"; exit 0; fi
host=$4
shift 4
case "$*" in
    ping) echo PONG ;;
    'client info') echo 'laddr=10.0.0.1:6379' ;;
    'cluster info')
        if [ -f "$TEST_DIR/created" ] || [ "$SCENARIO" = existing ] ||
           { [ "$SCENARIO" = partial ] && [ "${host%%.*}" != valkey-0 ]; }; then
            printf '%s\n' cluster_state:ok cluster_slots_assigned:16384 cluster_slots_ok:16384 cluster_slots_fail:0 cluster_slots_pfail:0 cluster_known_nodes:6 cluster_size:3
        else
            printf '%s\n' cluster_known_nodes:1 cluster_slots_assigned:0
        fi ;;
    dbsize) if [ "$SCENARIO" = nonempty ]; then echo 1; else echo 0; fi ;;
    'cluster meet '*) echo OK ;;
    'info replication') printf '%s\n' role:slave master_link_status:up master_sync_in_progress:0 ;;
    'cluster nodes') printf '%s\n' 'a addr master -' 'b addr master -' 'c addr master -' 'd addr slave a' 'e addr slave b' 'f addr slave c' ;;
    *) exit 2 ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "valkey-cli"), []byte(cli), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), "/bin/sh", "-ec", bootstrapValkey)
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "TEST_DIR="+dir, "SCENARIO="+scenario)
			output, err := cmd.CombinedOutput()
			wantError := scenario == "partial" || scenario == "nonempty"
			if (err != nil) != wantError {
				t.Fatalf("bootstrap: %v\n%s", err, output)
			}
			calls, err := os.ReadFile(filepath.Join(dir, "calls"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(calls), "--cluster create") != (scenario == "fresh") {
				t.Fatalf("unexpected cluster creation:\n%s", calls)
			}
			if strings.Contains(string(calls), "cluster meet") != (scenario == "existing") {
				t.Fatalf("unexpected peer refresh:\n%s", calls)
			}
		})
	}
}
