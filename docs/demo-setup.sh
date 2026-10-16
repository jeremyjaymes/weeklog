#!/usr/bin/env bash
# Prepare an isolated weeklog config and database for docs/demo.tape.
#
# Source this from the repository root. It builds weeklog, writes the
# fictional fixture from docs/demoseed, and exports XDG_* and PATH for that
# process. The fixture has no tokens and no personal activity.

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  echo "source docs/demo-setup.sh from the repository root" >&2
  exit 1
fi

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root"

demo_home=${WEEKLOG_DEMO_HOME:-/tmp/weeklog-demo}
rm -rf "$demo_home"
mkdir -p "$demo_home/bin" "$demo_home/config/weeklog" "$demo_home/data/weeklog" "$demo_home/cache"

export XDG_CONFIG_HOME="$demo_home/config"
export XDG_DATA_HOME="$demo_home/data"
export XDG_CACHE_HOME="$demo_home/cache"
export TZ=UTC
export PATH="$demo_home/bin:${PATH}"
# VHS starts bash with its own prompt. Pin a short one so the GIF does not
# show the operator's host, path, or shell theme.
export PS1='> '
unset GH_TOKEN GITHUB_TOKEN GH_ENTERPRISE_TOKEN LINEAR_API_KEY LINEAR_TOKEN NO_COLOR

go build -o "$demo_home/bin/weeklog" ./cmd/weeklog
go run ./docs/demoseed -db "$XDG_DATA_HOME/weeklog/weeklog.db"

cat > "$XDG_CONFIG_HOME/weeklog/config.toml" <<'EOF'
[identity]
timezone = "UTC"
week_starts = "monday"

[output]
mode = "terminal"
EOF
