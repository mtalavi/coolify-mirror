"""Test-lab only: populate the *source* Coolify with realistic resources.

Creates two projects with an image app (volume + file mount + env vars that
reference a Postgres), a git/Dockerfile app, a standalone Postgres with data,
a one-click service with a domain, tags, a scheduled task and a shared var.
"""
import json
import os
import sys
import time
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
creds = {}
for line in open(os.path.join(HERE, ".lab-credentials"), encoding="utf-8"):
    k, _, v = line.strip().partition("=")
    creds[k] = v.strip("'")

BASE = os.environ.get("LAB_BASE", "http://localhost:18000/api/v1")
TOKEN = creds[os.environ.get("LAB_TOKEN_KEY", "CMLAB_SRC_TOKEN")]


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
print("server", server["uuid"], server["is_reachable"], server["is_usable"])
api("POST", f"/servers/{server['uuid']}/proxy/restart")


def project(name):
    p = api("POST", "/projects", {"name": name, "description": f"{name} test project"})
    envs = api("GET", f"/projects/{p['uuid']}/environments")
    env = [e for e in envs if e["name"] == "production"][0]
    return p["uuid"], env["uuid"]


shop_uuid, shop_env = project("Shop")
blog_uuid, blog_env = project("Blog")
common = {"server_uuid": server["uuid"], "environment_name": "production"}

# Project-level shared variable referenced from an app ({{project.SHOP_CURRENCY}}).
api("POST", f"/projects/{shop_uuid}/envs", {"key": "SHOP_CURRENCY", "value": "EUR"})

db = api("POST", "/databases/postgresql", {
    **common, "project_uuid": shop_uuid, "environment_uuid": shop_env,
    "name": "shop-db", "postgres_user": "shop", "postgres_password": "S3cret-db-pass",
    "postgres_db": "shop", "instant_deploy": True,
})
print("postgres", db)

app = api("POST", "/applications/dockerimage", {
    **common, "project_uuid": shop_uuid, "environment_uuid": shop_env,
    "name": "shop-web", "docker_registry_image_name": "nginx",
    "docker_registry_image_tag": "alpine", "ports_exposes": "80",
    "domains": "http://shop.cmlab.test,http://www.shop.cmlab.test",
    "instant_deploy": False,
})
print("image app", app)
au = app["uuid"]
for env in [
    {"key": "DATABASE_URL", "value": f"postgres://shop:S3cret-db-pass@{db['uuid']}:5432/shop"},
    {"key": "SECRET_KEY", "value": "top-secret-value-123"},
    {"key": "CURRENCY", "value": "{{project.SHOP_CURRENCY}}"},
    {"key": "MULTI", "value": "line1\nline2", "is_multiline": True},
]:
    api("POST", f"/applications/{au}/envs", env)
api("POST", f"/applications/{au}/storages", {"type": "persistent", "name": "html", "mount_path": "/usr/share/nginx/html"})
api("POST", f"/applications/{au}/storages", {
    "type": "file", "mount_path": "/etc/nginx/conf.d/health.conf",
    "content": "server { listen 8081; location / { return 200 'lab-ok'; } }\n",
})
print("deploy image app", api("POST", "/deploy", {"uuid": au}))

gitapp = api("POST", "/applications/public", {
    **common, "project_uuid": shop_uuid, "environment_uuid": shop_env,
    "name": "shop-api", "git_repository": "https://github.com/coollabsio/coolify-examples",
    "git_branch": "main", "build_pack": "dockerfile", "base_directory": "/dockerfile/single-stage",
    "ports_exposes": "80", "domains": "http://api.shop.cmlab.test", "instant_deploy": True,
})
print("git app", gitapp)

svc = api("POST", "/services", {
    **common, "project_uuid": blog_uuid, "environment_uuid": blog_env,
    "type": "uptime-kuma", "name": "status",
    "urls": [{"name": "uptime-kuma", "url": "http://status.cmlab.test"}],
    "instant_deploy": True,
})
print("service", svc)

out = {"db": db["uuid"], "app": au, "gitapp": gitapp["uuid"], "service": svc["uuid"],
       "shop_project": shop_uuid, "blog_project": blog_uuid}
json.dump(out, open(os.path.join(HERE, ".lab-src-resources.json"), "w"), indent=1)
print(json.dumps(out, indent=1))
