# ~/.bashrc  (condensed)   q = launcher for everything below   guide = a 12-line card
export PATH="/usr/local/bin:/root/.local/bin:$PATH"
unset ANTHROPIC_BASE_URL ANTHROPIC_AUTH_TOKEN ANTHROPIC_API_KEY OMNIROUTE_BASE_URL OMNIROUTE_API_KEY GOOGLE_GEMINI_BASE_URL GEMINI_API_KEY
[[ $- != *i* ]] && return   # never print anything for scp/sftp/non-interactive ssh

# reload this file automatically when it is edited; reset a stuck touch-terminal mouse mode
_bashrc_mtime=$(stat -c %Y ~/.bashrc 2>/dev/null)
_auto_reload() { local m; m=$(stat -c %Y ~/.bashrc 2>/dev/null); [[ -n $m && $m -gt $_bashrc_mtime ]] && { _bashrc_mtime=$m; echo "~/.bashrc changed: reloading"; source ~/.bashrc; }; }
[[ $PROMPT_COMMAND == *_auto_reload* ]] || PROMPT_COMMAND="printf '\033[?1000l\033[?1006l'; _auto_reload${PROMPT_COMMAND:+; $PROMPT_COMMAND}"

# prompt: [WMS+PartDB] user@host:dir$   (root is red)
_c=32; [ "$(id -u)" -eq 0 ] && _c=31
PS1='\[\033[1;36m\][WMS+PartDB]\[\033[0m\] \[\033[1;'$_c'm\]\u@\h\[\033[0m\]:\[\033[1;34m\]\w\[\033[0m\]\$ '

# the tools: q opens the launcher (type to filter, Enter runs, Ctrl-P pins); guide prints the card
alias q='wms-go menu' guide='wms-go sys guide' wms='wms-go' tui='wms-go tui' gowms='wms-go'
alias modernwms='wms-go tui' partdb='wms-go tui' wms-tui='wms-go tui' partdb-tui='wms-go tui'
alias scripts='wms-go menu' script-runner='wms-go menu'
alias reload='source ~/.bashrc' src='reload'

# the old command names still work; each is one launcher entry (see: wms-go menu list)
alias status='wms-go sys status' sysstatus='wms-go sys status' syshealth='wms-go sys status'
alias audit='wms-go sys audit' audit-full='cat /root/tui_audit.log' audit-follow='wms-go sys audit -f'
alias wms-logs='wms-go menu run logs-wms' partdb-logs='wms-go menu run logs-partdb' sync-logs='wms-go menu run logs-sync'
alias wms-restart='wms-go menu run restart-wms' partdb-restart='wms-go menu run restart-partdb' sync-restart='wms-go menu run restart-sync' services-restart='wms-go menu run restart-all'
alias wms-go-doctor='wms-go doctor' wms-go-telnet-help='wms-go sys connect' wms-go-lego='wms-go lego'
alias wms-go-deploy='bash /root/modernwms-partdb-go/scripts/deploy.sh' wms-go-publish='bash /root/modernwms-partdb-go/scripts/publish.sh' preflight='wms-go preflight'
alias receive-stock='wms-go receive' reset-password='wms-go users reset' manage-users='wms-go users' wms-backup='wms-go backup'

# telnet: no args = pick an instance (1 live, 2 test, 3 QA scratch); with args it is the normal telnet
alias tnl='telnet 127.0.0.1 2323' tnt='telnet 127.0.0.1 2324' tnq='telnet 127.0.0.1 12323'
telnet() {
  [ $# -gt 0 ] && { command telnet "$@"; return; }
  local PS3="telnet to which? " o; select o in "live (2323)" "test (2324)" "QA scratch (12323)"; do
    case $REPLY in 1) command telnet 127.0.0.1 2323;; 2) command telnet 127.0.0.1 2324;; 3) command telnet 127.0.0.1 12323;; *) continue;; esac; break
  done
}

# shell basics
alias ls='ls --color=auto' ll='ls -la --color=auto' la='ls -A --color=auto' grep='grep --color=auto' df='df -h' du='du -h' free='free -h' ports='ss -tulpn'
alias rm='rm -i' cp='cp -i' mv='mv -i' dps='docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"'
alias cdwms='cd /root/docker-server' cdpartdb='cd /root/docker-server/partdb' cdsync='cd /root/partdb_modernwms_sync' cdbackups='cd /root/backups' cdgo='cd /root/modernwms-partdb-go'

command -v wms-go &>/dev/null && source <(wms-go completion bash 2>/dev/null)

# login banner: one line (containers, gateway, backup age, low stock)
if command -v wms-go &>/dev/null; then wms-go sys banner 2>/dev/null || echo "[WMS+PartDB] deploy the new wms-go for the launcher (q)"; fi
