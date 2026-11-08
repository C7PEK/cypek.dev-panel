package database

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// TuningPath is the MariaDB drop-in the installer owns. 60- sorts after the
// distribution's 50-server.cnf so these values win.
const TuningPath = "/etc/mysql/mariadb.conf.d/60-homepanel.cnf"

// TuningConfig renders MariaDB settings for game servers on homelab hardware.
//
// Why: FiveM's oxmysql and Minecraft plugins warn about "slow queries" on a
// stock install even though the queries themselves are trivial. The time
// goes elsewhere:
//   - reverse DNS lookups of every new connection (skip-name-resolve),
//   - an fsync of the redo log on every commit, which on SD cards, cheap SSDs
//     and thin-provisioned VM disks costs tens of milliseconds
//     (innodb_flush_log_at_trx_commit=2: at most ~1 s of commits can be lost
//     on a power cut, never on a crash of MariaDB itself),
//   - a buffer pool too small to keep the game's tables in memory.
func TuningConfig(totalRAMBytes uint64) string {
	pool := totalRAMBytes / 4
	const mb = 1024 * 1024
	if pool < 128*mb {
		pool = 128 * mb
	}
	if pool > 2048*mb {
		pool = 2048 * mb
	}
	pool = pool / (128 * mb) * (128 * mb) // multiples of the chunk size
	return fmt.Sprintf(`# Written by panel.cypek.dev (homepanel install-database). Re-running the
# installer rewrites this file; put your own overrides in 70-local.cnf.
[mysqld]
bind-address                   = 127.0.0.1
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
`, pool/mb)
}

// TotalRAM reads MemTotal from /proc/meminfo (bytes); 0 when unknown.
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
