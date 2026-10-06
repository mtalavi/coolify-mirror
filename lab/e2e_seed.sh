#!/usr/bin/env bash
# Runs inside the e2e source server: creates the resources that are migrated.
#   - a Docker Compose app built from git (this repository, lab/compose-app),
#     with a database, a one-shot migration, secrets and a domain;
#   - a standalone Postgres with rows;
#   - a Docker image app (nginx) with a volume and a domain.
# Prints the uuids as KEY=VALUE lines.
set -euo pipefail
TOKEN="$1"
REPO="${2:-https://github.com/mtalavi/coolify-mirror}"
BRANCH="${3:-main}"
API=http://localhost:8000/api/v1

api() {
  local method=$1 path=$2 body=${3:-}
  if [ -n "$body" ]; then
    curl -fsS -X "$method" "$API$path" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -H 'Accept: application/json' -d "$body"
  else
    curl -fsS -X "$method" "$API$path" -H "Authorization: Bearer $TOKEN" -H 'Accept: application/json'
  fi
}

server=$(api GET /servers | jq -r '.[0].uuid')
api POST "/servers/$server/proxy/restart" >/dev/null || true
project=$(api POST /projects '{"name":"E2E","description":"compatibility test"}' | jq -r .uuid)
env=$(api GET "/projects/$project/environments" | jq -r '.[] | select(.name=="production") | .uuid')
common="\"project_uuid\":\"$project\",\"environment_uuid\":\"$env\",\"server_uuid\":\"$server\",\"environment_name\":\"production\""

compose=$(api POST /applications/public "{$common,\"name\":\"e2e-compose\",\"git_repository\":\"$REPO\",\"git_branch\":\"$BRANCH\",
  \"build_pack\":\"dockercompose\",\"base_directory\":\"/lab/compose-app\",\"docker_compose_location\":\"/docker-compose.yaml\",
  \"ports_exposes\":\"8080\",\"instant_deploy\":false}" | jq -r .uuid)
# The API cannot set compose domains before the compose file is loaded from git.
docker exec -e U="$compose" -e DOMAIN=http://web.e2e.test:8080 coolify php /tmp/e2e_compose.php >/dev/null
# Loading the compose file already created these variables (empty): set them.
api PATCH "/applications/$compose/envs/bulk" '{"data":[{"key":"DB_PASSWORD","value":"E2e-db-Pa55"},{"key":"SECRET_TOKEN","value":"e2e-secret-token-42"}]}' >/dev/null

pg=$(api POST /databases/postgresql "{$common,\"name\":\"e2e-db\",\"postgres_user\":\"e2e\",\"postgres_password\":\"E2e-pg-Pa55\",\"postgres_db\":\"e2e\",\"instant_deploy\":true}" | jq -r .uuid)

web=$(api POST /applications/dockerimage "{$common,\"name\":\"e2e-web\",\"docker_registry_image_name\":\"nginx\",\"docker_registry_image_tag\":\"alpine\",
  \"ports_exposes\":\"80\",\"domains\":\"http://nginx.e2e.test\",\"instant_deploy\":false}" | jq -r .uuid)
api POST "/applications/$web/storages" '{"type":"persistent","name":"e2e-html","mount_path":"/usr/share/nginx/html"}' >/dev/null

api POST "/applications/$compose/start" >/dev/null
api POST "/applications/$web/start" >/dev/null
echo "COMPOSE=$compose"
echo "PG=$pg"
echo "WEB=$web"
