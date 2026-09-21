package tools

// Builtin is the catalogue that replaces the shell aliases. Order is the order shown when nothing is pinned
// or recent. Anything that restarts, replaces or changes data is Risky (asks yes/no first). Deploys and log
// followers are Shell-only: they must not run inside a telnet session, and a deploy would restart the very
// gateway carrying that session.
func Builtin() []Tool {
	self := "{self}"
	docker := func(id, title string, args ...string) Tool {
		return Tool{ID: id, Group: "Services", Title: title, Argv: append([]string{"docker"}, args...), Where: Both}
	}
	return []Tool{
		// Status
		{ID: "status", Group: "Status", Title: "System health", Hint: "memory, disk, containers, gateway, backups", Argv: []string{self, "sys", "status"}, Where: Both},
		{ID: "doctor", Group: "Status", Title: "Doctor: check the whole install", Hint: "services, tokens, clock, permissions, audit, backups", Argv: []string{self, "doctor"}, Where: Both},
		{ID: "audit", Group: "Status", Title: "Audit log: last 25 entries", Argv: []string{self, "sys", "audit", "-n", "25"}, Where: Both, Admin: true},
		{ID: "audit-verify", Group: "Status", Title: "Audit log: verify the hash chain", Argv: []string{self, "audit", "verify"}, Where: Both, Admin: true},
		{ID: "audit-follow", Group: "Status", Title: "Audit log: follow live", Argv: []string{self, "sys", "audit", "-f"}, Where: Shell},

		// Services
		docker("containers", "Containers", "ps", "--format", "table {{.Names}}\t{{.Status}}\t{{.Ports}}"),
		{ID: "gateway", Group: "Services", Title: "Telnet and web gateway: status", Argv: []string{"systemctl", "status", "wms-gateway.service", "--no-pager", "-n", "8"}, Where: Both},
		{ID: "connect", Group: "Services", Title: "How to connect from another machine", Argv: []string{self, "sys", "connect"}, Where: Both},
		{ID: "logs-wms", Group: "Services", Title: "Logs: ModernWMS (follow)", Argv: []string{"docker", "logs", "-f", "--tail=100", "modernwms"}, Where: Shell},
		{ID: "logs-partdb", Group: "Services", Title: "Logs: Part-DB (follow)", Argv: []string{"docker", "logs", "-f", "--tail=100", "partdb"}, Where: Shell},
		{ID: "logs-sync", Group: "Services", Title: "Logs: Part-DB sync (follow)", Argv: []string{"docker", "logs", "-f", "--tail=100", "partdb-sync"}, Where: Shell},
		{ID: "restart-wms", Group: "Services", Title: "Restart ModernWMS", Argv: []string{"docker", "restart", "modernwms"}, Risky: true, Admin: true, Where: Shell},
		{ID: "restart-partdb", Group: "Services", Title: "Restart Part-DB", Argv: []string{"docker", "restart", "partdb"}, Risky: true, Admin: true, Where: Shell},
		{ID: "restart-sync", Group: "Services", Title: "Restart Part-DB sync", Argv: []string{"docker", "restart", "partdb-sync"}, Risky: true, Admin: true, Where: Shell},
		{ID: "restart-all", Group: "Services", Title: "Restart ModernWMS, Part-DB and sync", Argv: []string{"docker", "restart", "modernwms", "partdb", "partdb-sync"}, Risky: true, Admin: true, Where: Shell},
		{ID: "restart-gateway", Group: "Services", Title: "Restart the telnet/web gateway", Hint: "drops connected sessions", Argv: []string{"systemctl", "restart", "wms-gateway.service"}, Risky: true, Admin: true, Where: Shell},
		{ID: "sync-status", Group: "Services", Title: "Part-DB sync: last run", Argv: []string{self, "sync", "status"}, Where: Both},
		{ID: "sync-now", Group: "Services", Title: "Part-DB sync: run one cycle now", Argv: []string{self, "sync", "trigger"}, Risky: true, Admin: true, Where: Both},

		// Backups
		{ID: "backup", Group: "Backups", Title: "Back up ModernWMS", Argv: []string{self, "backup"}, Risky: true, Admin: true, Where: Both},
		{ID: "backup-lego", Group: "Backups", Title: "Back up the LEGO collection", Argv: []string{self, "lego", "backup"}, Admin: true, Where: Both},

		// People and stock
		{ID: "users", Group: "People", Title: "Users: list everyone", Argv: []string{self, "users", "list"}, Admin: true, Where: Both},
		{ID: "user-reset", Group: "People", Title: "Users: reset a password", Hint: "prints a temporary password", Argv: []string{self, "users", "reset", "{0}"}, Ask: []string{"User name or id"}, Risky: true, Admin: true, Where: Shell},
		{ID: "user-unlock", Group: "People", Title: "Users: clear a 2FA lockout", Argv: []string{self, "users", "2fa", "unlock", "{0}"}, Ask: []string{"User name"}, Admin: true, Where: Shell},
		{ID: "receive", Group: "People", Title: "Receive stock", Hint: "part name, code or id, then quantity", Argv: []string{self, "receive", "{0}", "{1}"}, Ask: []string{"Part name, code or id", "Quantity"}, Risky: true, Admin: true, Where: Shell},

		// LEGO
		{ID: "lego-stats", Group: "LEGO", Title: "Collection stats", Argv: []string{self, "lego", "stats"}, Where: Both},
		{ID: "lego-low", Group: "LEGO", Title: "Parts below their minimum", Argv: []string{self, "lego", "low"}, Where: Both},
		{ID: "lego-value", Group: "LEGO", Title: "Collection value (BrickLink prices)", Argv: []string{self, "lego", "value"}, Where: Both},
		{ID: "lego-watch", Group: "LEGO", Title: "Check price watches", Hint: "uses BrickLink calls", Argv: []string{self, "lego", "watch", "check"}, Admin: true, Where: Both},
		{ID: "lego-build", Group: "LEGO", Title: "What can I build?", Argv: []string{self, "lego", "build"}, Where: Both},
		{ID: "lego-history", Group: "LEGO", Title: "History of changes", Argv: []string{self, "lego", "history", "--limit", "15"}, Where: Both},
		{ID: "lego-catalog", Group: "LEGO", Title: "Offline catalog: status", Argv: []string{self, "lego", "catalog", "status"}, Where: Both},
		{ID: "lego-refresh", Group: "LEGO", Title: "Offline catalog: refresh now", Hint: "downloads about 16 MB", Argv: []string{self, "lego", "catalog", "refresh"}, Admin: true, Where: Both},

		// Ship
		{ID: "preflight", Group: "Ship", Title: "Preflight: is it safe to ship?", Hint: "GO or NO-GO for deploy and publish", Argv: []string{self, "preflight", "all", "--repo", "{repo}"}, Where: Shell},
		{ID: "update", Group: "Ship", Title: "Check for a new release", Argv: []string{self, "update"}, Where: Both},
		{ID: "deploy", Group: "Ship", Title: "Deploy to the live gateway", Hint: "backs up, swaps the binary, restarts", Argv: []string{"bash", "{repo}/scripts/deploy.sh"}, Risky: true, Admin: true, Where: Shell},
		{ID: "publish", Group: "Ship", Title: "Publish to GitHub", Hint: "creates the public repo, docs and release", Argv: []string{"bash", "{repo}/scripts/publish.sh"}, Risky: true, Admin: true, Where: Shell},

		// Help
		{ID: "guide", Group: "Help", Title: "Quick guide", Argv: []string{self, "sys", "guide"}, Where: Both},
	}
}
