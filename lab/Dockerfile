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
      iptables kmod procps \
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

# Keep the cpu/memory/io/pids controllers enabled for every unit. Without
# this, systemd may leave cpu out of system.slice and Docker cannot start a
# container with a CPU weight (Coolify sets one): "cpu.weight: no such file".
COPY accounting.conf /etc/systemd/system.conf.d/accounting.conf

STOPSIGNAL SIGRTMIN+3
VOLUME ["/var/lib/docker"]
CMD ["/sbin/init"]
