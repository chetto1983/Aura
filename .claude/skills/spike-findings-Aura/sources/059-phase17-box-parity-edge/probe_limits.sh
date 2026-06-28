printf "mem.max="; cat /sys/fs/cgroup/memory.max
printf "pids.max="; cat /sys/fs/cgroup/pids.max
printf "cpu.max="; cat /sys/fs/cgroup/cpu.max
printf "nproc_visible="; nproc
