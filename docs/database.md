# Databases (MariaDB + phpMyAdmin add-on)

FiveM resources (oxmysql, ESX, QBCore) and many Minecraft plugins
(LuckPerms, CoreProtect, …) need MySQL/MariaDB. The panel can install it and
create one database + one database user per request for any server.

## Install

Panel menu → Databases → Install (owner only). The button starts
`homepanel-database-install.service`, a oneshot unit that runs
`homepanel install-database` as root (the panel itself never runs as root;
polkit lets it start exactly this unit). From a terminal the same is:

```sh
sudo homepanel install-database [--phpmyadmin-port 8081]
```

It is idempotent; run it again to repair. Steps:

1. `apt-get install` MariaDB server/client, PHP CLI with the extensions
   phpMyAdmin needs, and phpMyAdmin (Debian packages, no Apache).
2. Writes `/etc/mysql/mariadb.conf.d/60-homepanel.cnf` and restarts MariaDB.
3. Creates `homepanel_admin@localhost` with all privileges on `hp\_%`
   databases only (plus CREATE USER to manage per-server accounts).
4. Runs phpMyAdmin with PHP's built-in server as `homepanel-phpmyadmin.service`
   (user www-data, port 8081).
5. Saves the admin credentials to `/etc/homepanel/database.json` (0640 root:homepanel).

## Tuning (why oxmysql stops warning about slow queries)

Measured on a thin-provisioned VM disk: 200 single-row INSERTs took
**104 ms each** with the stock settings and **0.1-2 ms** with these:

| Setting | Value | Why |
|---|---|---|
| `innodb_flush_log_at_trx_commit` | 2 | no fsync per commit; at most ~1 s of commits lost on a power cut (not on a MariaDB crash) |
| `skip-name-resolve` | ON | no reverse DNS on every new connection |
| `innodb_buffer_pool_size` | 25% of RAM (128 MB to 2 GB) | keeps game tables in memory |
| `bind-address` | 127.0.0.1,::1 (127.0.0.1 before MariaDB 10.11) | loopback only; both families so FXServer's "localhost" (often ::1) connects instantly |
| `performance_schema` | OFF | saves ~100 MB RAM |
| `slow_query_log` / `long_query_time` | ON / 1 s | real slow queries still land in `/var/log/mysql/mariadb-slow.log` |

Put your own overrides in `/etc/mysql/mariadb.conf.d/70-local.cnf`; the
installer rewrites `60-homepanel.cnf` on every run.

## FiveM defaults

The FiveM templates keep `set mysql_slow_query_warning 1500` in `server.cfg`
and `txData/*/server.cfg` (added before every start when missing, never
overriding your own value). oxmysql times queries inside FXServer, so a
server hitch looks like a slow query at its 200 ms default even when MariaDB
answered in a millisecond. Database accounts exist for `localhost`,
`127.0.0.1` and `::1`, so any of the three works as host.

## Per-server databases

Server → Databases → Create. Names are `hp_<name>`; the user has
the same name and access to that database only, from `localhost` (socket,
phpMyAdmin) and `127.0.0.1` (TCP, game servers). The password is random,
stored encrypted, revealed on demand (audited) and can be rotated.

FiveM (`server.cfg`):

```
set mysql_connection_string "mysql://hp_esx:PASSWORD@127.0.0.1:3306/hp_esx?charset=utf8mb4"
```

Deleting a server together with its files also drops its databases.
