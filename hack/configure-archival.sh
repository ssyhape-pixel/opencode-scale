#!/usr/bin/env bash
# configure-archival.sh — Enable archival on the Temporal "default" namespace.
# Run inside the Temporal container or with tctl configured to reach the cluster.
set -euo pipefail

RETENTION="${ARCHIVAL_RETENTION:-72h}"
NAMESPACE="${TEMPORAL_NAMESPACE:-default}"
TCTL_ADDR="${TCTL_ADDR:-$(hostname -i):7233}"

echo "Configuring namespace '${NAMESPACE}' for archival..."
echo "  Retention:  ${RETENTION}"
echo "  Address:    ${TCTL_ADDR}"

tctl --address "${TCTL_ADDR}" --namespace "${NAMESPACE}" \
  namespace update \
  --retention "${RETENTION}" \
  --history_archival_state enabled \
  --visibility_archival_state enabled

echo ""
echo "Archival configured. Verifying..."
tctl --address "${TCTL_ADDR}" --namespace "${NAMESPACE}" \
  namespace describe \
  | grep -E '(HistoryArchival|VisibilityArchival|Retention)'

echo ""
echo "Done."
