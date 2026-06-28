echo "shell=OK"
printf "uid="; id -u
printf "py="; python3 --version
printf "node="; node --version
echo hello > /opt/selfextend && printf "writable_rootfs=" && cat /opt/selfextend
sleep 5 & printf "subprocess_spawned_pid="; echo $!
echo "--- host edge (the box stays a box) ---"
ls -l /var/run/docker.sock 2>&1 || true
command -v docker >/dev/null && echo "HAS-DOCKER" || echo "NO-DOCKER-BINARY"
printf "rootfs="; grep PRETTY_NAME /etc/os-release
test -e /host && echo "host_visible=YES" || echo "host_root_path_/host=absent (host fs not mounted)"
