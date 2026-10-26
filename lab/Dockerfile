# Test lab only: an Ubuntu 24.04 "server" with systemd as PID 1, so the
# official Coolify installer can run inside it exactly like on a real VPS.
# Two of these (source + target) are used for end-to-end backup/restore tests.
FROM ubuntu:24.04

ENV container=docker \
    DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      systemd systemd-sysv dbus openssh-server curl wget ca-certificates git jq \
      openssl iproute2 iputils-ping sudo gnupg lsb-release cron less nano \
      iptables kmod procps rclone \
 && apt-get clean && rm -rf /var/lib/apt/lists/*

# Strip units that make no sense inside a container.
RUN cd /lib/systemd/system/sysinit.target.wants/ \
 && ls | grep -v systemd-tmpfiles-setup | xargs rm -f \
 && rm -f /lib/systemd/system/multi-user.target.wants/* \
          /etc/systemd/system/*.wants/* \
          /lib/systemd/system/local-fs.target.wants/* \
          /lib/systemd/system/sockets.target.wants/*udev* \
          /lib/systemd/system/sockets.target.wants/*initctl* \
          /lib/systemd/system/basic.target.wants/* \
 && systemctl enable ssh

# Enable the cpu controller at boot, before any `docker exec` process sits in
# the root cgroup (which would make enabling it later fail with EBUSY). Docker
# needs it for containers with a CPU weight, which Coolify sets:
# "cpu.weight: no such file or directory" otherwise.
COPY system-slice-cpu.conf /etc/systemd/system/system.slice.d/cpu.conf

# Pull Docker Hub images through Google's public mirror: the daily tests pull
# the same images again and again and hit Docker Hub's anonymous rate limit.
# Coolify's installer keeps these keys when it adds its own.
COPY docker-daemon.json /etc/docker/daemon.json

STOPSIGNAL SIGRTMIN+3
VOLUME ["/var/lib/docker"]
CMD ["/sbin/init"]
