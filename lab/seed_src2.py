"""Test-lab only: add harder cases to the source Coolify.

WordPress + MariaDB service (service databases, several volumes), a Redis on a
custom docker network destination, tags, a scheduled task, an S3 storage with a
scheduled database backup and a volume backup schedule.
"""
import json
import os
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
creds = {}
for line in open(os.path.join(HERE, ".lab-credentials"), encoding="utf-8"):
    k, _, v = line.strip().partition("=")
    creds[k] = v.strip("'")
BASE = "http://localhost:18000/api/v1"
TOKEN = creds["CMLAB_SRC_TOKEN"]
res = json.load(open(os.path.join(HERE, ".lab-src-resources.json")))


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
        print(method, path, "->", e.code, e.read().decode()[:600])
        raise
    return json.loads(raw) if raw else {}


server = api("GET", "/servers")[0]
blog_envs = api("GET", f"/projects/{res['blog_project']}/environments")
blog_env = [e for e in blog_envs if e["name"] == "production"][0]["uuid"]
shop_envs = api("GET", f"/projects/{res['shop_project']}/environments")
shop_env = [e for e in shop_envs if e["name"] == "production"][0]["uuid"]

wp = api("POST", "/services", {
    "server_uuid": server["uuid"], "project_uuid": res["blog_project"], "environment_name": "production",
    "environment_uuid": blog_env, "type": "wordpress-with-mariadb", "name": "blog",
    "urls": [{"name": "wordpress", "url": "http://blog.cmlab.test"}], "instant_deploy": True,
})
print("wordpress", wp)

dest = api("POST", f"/servers/{server['uuid']}/destinations", {"name": "lab-net", "network": "lab-net", "type": "standalone"})
print("destination", dest)
redis = api("POST", "/databases/redis", {
    "server_uuid": server["uuid"], "project_uuid": res["shop_project"], "environment_name": "production",
    "environment_uuid": shop_env, "name": "shop-cache", "destination_uuid": dest["uuid"], "instant_deploy": True,
})
print("redis", redis)

print(api("POST", f"/applications/{res['app']}/tags", {"tag_names": ["frontend", "shop"]}))
print(api("POST", f"/applications/{res['app']}/scheduled-tasks", {
    "name": "cleanup", "command": "echo cleanup", "frequency": "0 3 * * *"}))
s3 = api("POST", "/s3-storages", {"name": "lab-s3", "endpoint": "https://s3.example.com", "bucket": "lab-bucket",
                                  "region": "eu-central-1", "key": "AKIALABKEY", "secret": "lab-secret-value", "is_usable": True})
print("s3", s3)
print(api("POST", f"/databases/{res['db']}/backups", {"frequency": "0 2 * * *", "save_s3": True,
                                                       "s3_storage_uuid": s3["uuid"], "enabled": True}))
storages = api("GET", f"/applications/{res['app']}/storages")
html = [s for s in storages["persistent_storages"] if s["mount_path"] == "/usr/share/nginx/html"][0]
print(api("PUT", f"/applications/{res['app']}/storages/{html['uuid']}/backups", {"frequency": "0 4 * * *", "enabled": True}))

res.update({"wordpress": wp["uuid"], "redis": redis["uuid"], "destination": dest["uuid"], "s3": s3["uuid"]})
json.dump(res, open(os.path.join(HERE, ".lab-src-resources.json"), "w"), indent=1)
