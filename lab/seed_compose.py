"""Test-lab only: add a Docker Compose application built from git whose build
depends on the host (a policy script under /data/coolify/ops and a named
buildx builder), like real setups that bound their builds. Used to check that
a restored copy can be deployed again on the target without manual fixes.

Usage: LAB_GIT=http://<git server ip>/labapp.git python lab/seed_compose.py
"""
import json
import os
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
creds = {}
for line in open(os.path.join(HERE, ".lab-credentials"), encoding="utf-8"):
    k, _, v = line.strip().partition("=")
    creds[k] = v.strip("'")

BASE = os.environ.get("LAB_BASE", "http://localhost:18000/api/v1")
TOKEN = creds[os.environ.get("LAB_TOKEN_KEY", "CMLAB_SRC_TOKEN")]
GIT = os.environ["LAB_GIT"]
DEST = os.environ.get("LAB_DEST", "")  # destination uuid when the server has several


def api(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Authorization", "Bearer " + TOKEN)
    req.add_header("Content-Type", "application/json")
    req.add_header("Accept", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=120) as r:
            raw = r.read().decode()
    except urllib.error.HTTPError as e:
        print(method, path, "->", e.code, e.read().decode()[:800])
        raise
    return json.loads(raw) if raw else {}


server = api("GET", "/servers")[0]
p = next((x for x in api("GET", "/projects") if x["name"] == "Compose Lab"), None) or \
    api("POST", "/projects", {"name": "Compose Lab", "description": "git compose app with host build policy"})
envs = api("GET", f"/projects/{p['uuid']}/environments")
env = [e for e in envs if e["name"] == "production"][0]

app = api("POST", "/applications/public", {
    "project_uuid": p["uuid"], "environment_uuid": env["uuid"], "server_uuid": server["uuid"],
    "environment_name": "production", "name": "labapp",
    "git_repository": GIT, "git_branch": "main", "build_pack": "dockercompose",
    "ports_exposes": "8080", "docker_compose_location": "/docker-compose.yaml",
    "instant_deploy": False, **({"destination_uuid": DEST} if DEST else {}),
})
uuid = app["uuid"]
print("app", uuid)
api("PATCH", f"/applications/{uuid}", {
    "docker_compose_custom_build_command":
        f"bash .ops-build.sh docker compose --project-name {uuid} build --pull --builder cm-bounded",
    "docker_compose_domains": [{"name": "web", "domain": "http://labapp.cmlab.test:8080"}],
})
for k, v in [("DB_PASSWORD", "Lab-db-Pa55"), ("SECRET_TOKEN", "lab-secret-token-42")]:
    api("POST", f"/applications/{uuid}/envs", {"key": k, "value": v})
print(json.dumps({"uuid": uuid, "project": p["uuid"]}))
