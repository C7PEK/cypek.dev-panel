package database

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const TuningPath = "/etc/mysql/mariadb.conf.d/60-homepanel.cnf"

func TuningConfig(totalRAMBytes uint64, dualStackBind bool) string {
	bind := "127.0.0.1"
	if dualStackBind {
		bind = "127.0.0.1,::1"
	}
	pool := totalRAMBytes / 4
	const mb = 1024 * 1024
	if pool < 128*mb {
		pool = 128 * mb
	}
	if pool > 2048*mb {
		pool = 2048 * mb
	}
	pool = pool / (128 * mb) * (128 * mb)
	return fmt.Sprintf(`# Written by panel.cypek.dev (homepanel install-database). Re-running the
# installer rewrites this file; put your own overrides in 70-local.cnf.
[mysqld]
bind-address                   = %s
skip-name-resolve              = ON
character-set-server           = utf8mb4
collation-server               = utf8mb4_unicode_ci

innodb_buffer_pool_size        = %dM
innodb_flush_log_at_trx_commit = 2
innodb_flush_method            = O_DIRECT
innodb_log_file_size           = 256M
innodb_io_capacity             = 1000

max_connections                = 200
wait_timeout                   = 28800
max_allowed_packet             = 64M
table_open_cache               = 2000
tmp_table_size                 = 64M
max_heap_table_size            = 64M

# Saves ~100 MB of RAM on small hosts.
performance_schema             = OFF

# Real slow queries (over 1 s) still get logged for troubleshooting.
slow_query_log                 = ON
slow_query_log_file            = /var/log/mysql/mariadb-slow.log
long_query_time                = 1
`, bind, pool/mb)
}

func TotalRAM() uint64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				kb, _ := strconv.ParseUint(f[1], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}

func SupportsBindList(version string) bool {
	var major, minor int
	if _, err := fmt.Sscanf(version, "%d.%d", &major, &minor); err != nil {
		return false
	}
	return major > 10 || (major == 10 && minor >= 11)
}

func InstalledServerVersion() string {
	for _, bin := range []string{"/usr/sbin/mariadbd", "/usr/sbin/mysqld"} {
		out, err := exec.Command(bin, "--version").Output()
		if err != nil {
			continue
		}

		f := strings.Fields(string(out))
		for i, w := range f {
			if w == "Ver" && i+1 < len(f) {
				return f[i+1]
			}
		}
	}
	return ""
}
