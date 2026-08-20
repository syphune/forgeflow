#!/usr/bin/env sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
root_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
api_port=${FORGEFLOW_API_PORT:-18080}
web_port=${FORGEFLOW_WEB_PORT:-13000}

if [ -f "$root_dir/.env.local" ]; then
	set -a
	. "$root_dir/.env.local"
	set +a
fi

export DATABASE_URL="${DATABASE_URL:-postgres://forgeflow:forgeflow@localhost:5432/forgeflow?sslmode=disable}"
export FORGEFLOW_HTTP_ADDRESS=":$api_port"
export FORGEFLOW_WEB_BASE_URL="http://localhost:$web_port"
export FORGEFLOW_SECURE_COOKIES=false
export FORGEFLOW_GITHUB_OAUTH_REDIRECT_URL="http://localhost:$api_port/api/v1/auth/github/callback"
export FORGEFLOW_BACKEND_URL="http://localhost:$api_port"

compose_file="$root_dir/infra/compose.dev.yaml"
docker compose -f "$compose_file" up -d postgres

attempt=0
until docker compose -f "$compose_file" exec -T postgres pg_isready -U forgeflow -d forgeflow >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 30 ]; then
		printf '%s\n' "PostgreSQL did not become ready." >&2
		exit 1
	fi
	sleep 1
done

(cd "$root_dir/backend" && go run ./cmd/migrate up)

(cd "$root_dir/backend" && exec go run ./cmd/api) &
api_pid=$!

attempt=0
until curl -fsS "http://localhost:$api_port/health/live" >/dev/null 2>&1; do
	attempt=$((attempt + 1))
	if [ "$attempt" -ge 30 ]; then
		printf '%s\n' "Forgeflow API did not become ready." >&2
		exit 1
	fi
	sleep 1
done

(cd "$root_dir" && exec npm exec --yes --package=pnpm@10.14.0 -- pnpm --filter @forgeflow/web exec next dev -p "$web_port") &
web_pid=$!

cleanup() {
	status=$?
	trap - EXIT INT TERM
	kill "$api_pid" "$web_pid" 2>/dev/null || true
	wait "$api_pid" "$web_pid" 2>/dev/null || true
	exit "$status"
}
trap cleanup EXIT INT TERM

printf '%s\n' "Forgeflow running at http://localhost:$web_port"
printf '%s\n' "API health: http://localhost:$api_port/health/live"
printf '%s\n' "Press Ctrl-C to stop API and Web."

while :; do
	if ! kill -0 "$api_pid" 2>/dev/null; then
		wait "$api_pid"
		exit $?
	fi
	if ! kill -0 "$web_pid" 2>/dev/null; then
		wait "$web_pid"
		exit $?
	fi
	sleep 1
done
