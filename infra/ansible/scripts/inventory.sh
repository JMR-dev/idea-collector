#!/usr/bin/env bash
# Generate the Ansible inventory from Pulumi stack outputs.
#   ./scripts/inventory.sh [stack]   (default: prod)
set -euo pipefail

stack="${1:-prod}"
here="$(cd "$(dirname "$0")" && pwd)"
pulumi_dir="$here/../../pulumi"
out="$here/../inventory/hosts.generated.ini"

ip=$(pulumi -C "$pulumi_dir" stack output serverIPv4 --stack "$stack")
user=$(pulumi -C "$pulumi_dir" stack output sshUser --stack "$stack")

mkdir -p "$(dirname "$out")"
cat > "$out" <<EOF
[idea_collect]
$ip ansible_user=$user
EOF

echo "wrote $out:"
cat "$out"
