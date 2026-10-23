# Printed by lab/e2e.sh when the restore fails: what the target server looked
# like (memory, disk, Docker, containers that are not running and why, the
# coolify-mirror restore log and Coolify's last deployment logs).
free -m | sed -n 1,2p
df -h / | tail -1
echo "cgroup: $(cat /sys/fs/cgroup/cgroup.controllers) | enabled: $(cat /sys/fs/cgroup/cgroup.subtree_control)"
docker version --format 'docker {{.Server.Version}}' 2>&1
docker ps -a --format '{{.Names}}  {{.Image}}  {{.Status}}'
for c in $(docker ps -a --filter status=created --filter status=exited --filter status=restarting --filter status=dead -q); do
  echo "## $(docker inspect -f '{{.Name}} {{.State.Status}} exit={{.State.ExitCode}} oom={{.State.OOMKilled}} error={{.State.Error}}' "$c")"
  docker logs --tail 15 "$c" 2>&1
done
log=$(ls -t /data/coolify-mirror/logs/*restore.log 2>/dev/null | head -1)
[ -n "$log" ] && { echo "## $log"; tail -60 "$log"; }
echo "## last Coolify deployments"
docker exec coolify php artisan tinker --execute='foreach (\App\Models\ApplicationDeploymentQueue::latest()->take(3)->get() as $d) { echo "-- ".$d->application_name." ".$d->status.PHP_EOL; foreach (array_slice(json_decode($d->logs, true) ?: [], -25) as $l) { echo substr($l["output"] ?? "", 0, 400).PHP_EOL; } }' 2>&1 | tail -90
