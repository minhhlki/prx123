#!/usr/bin/env bash
set -Eeuo pipefail

#
# Edit only these values before running on the VPS.
#
REPO_URL="https://github.com/minhhlki/prx123.git"
REPO_BRANCH=""
APP_NAME="zmap-proxy-scanner"
APP_SUBDIR=""
SCANNER_ARGS="-cfg config.json -in pc.txt"
GO_VERSION="1.22.12"

#
# Optional overrides via environment variables.
#
BASE_DIR="${BASE_DIR:-$HOME/apps}"
LOG_DIR="${LOG_DIR:-$HOME/.local/var/$APP_NAME}"
GO_ROOT="${GO_ROOT:-$HOME/.local/go}"
GO_BIN_DIR="${GO_BIN_DIR:-$HOME/.local/bin}"
INSTALL_DIR="${INSTALL_DIR:-$BASE_DIR/$APP_NAME}"
REPO_DIR="$INSTALL_DIR/repo"
RUN_DIR="$REPO_DIR"
BIN_PATH="$INSTALL_DIR/$APP_NAME"
RUN_SCRIPT="$INSTALL_DIR/run.sh"
PID_FILE="$LOG_DIR/$APP_NAME.pid"
LOG_FILE="$LOG_DIR/$APP_NAME.log"
SERVICE_FILE="/etc/systemd/system/$APP_NAME.service"

if [[ -n "$APP_SUBDIR" ]]; then
  RUN_DIR="$REPO_DIR/$APP_SUBDIR"
fi

command_exists() {
  command -v "$1" >/dev/null 2>&1
}

run_as_root() {
  if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
    "$@"
  elif command_exists sudo; then
    sudo "$@"
  else
    return 1
  fi
}

ensure_dirs() {
  mkdir -p "$BASE_DIR" "$INSTALL_DIR" "$LOG_DIR" "$GO_BIN_DIR"
}

ensure_repo_url() {
  if [[ "$REPO_URL" == *"YOUR-USER"* ]] || [[ "$REPO_URL" == *"YOUR-REPO"* ]] || [[ -z "$REPO_URL" ]]; then
    echo "Set REPO_URL in deploy-vps.sh before running it."
    exit 1
  fi
}

ensure_git() {
  if command_exists git; then
    return
  fi

  echo "git not found, trying to install it..."
  if command_exists apt-get; then
    run_as_root apt-get update -y
    run_as_root apt-get install -y git curl tar
    return
  fi
  if command_exists dnf; then
    run_as_root dnf install -y git curl tar
    return
  fi
  if command_exists yum; then
    run_as_root yum install -y git curl tar
    return
  fi
  if command_exists apk; then
    run_as_root apk add --no-cache git curl tar
    return
  fi

  echo "Unable to install git automatically. Install git manually and rerun."
  exit 1
}

install_go_local() {
  local go_archive="go${GO_VERSION}.linux-amd64.tar.gz"
  local go_url="https://go.dev/dl/${go_archive}"
  local tmp_archive

  tmp_archive="$(mktemp)"
  echo "Installing Go ${GO_VERSION} into $GO_ROOT"
  curl -fsSL "$go_url" -o "$tmp_archive"
  rm -rf "$GO_ROOT"
  mkdir -p "$(dirname "$GO_ROOT")"
  tar -C "$(dirname "$GO_ROOT")" -xzf "$tmp_archive"
  rm -f "$tmp_archive"
  ln -sf "$GO_ROOT/bin/go" "$GO_BIN_DIR/go"
  ln -sf "$GO_ROOT/bin/gofmt" "$GO_BIN_DIR/gofmt"
}

ensure_go() {
  export PATH="$GO_BIN_DIR:$GO_ROOT/bin:$PATH"

  if command_exists go; then
    return
  fi

  if ! command_exists curl; then
    echo "curl not found, trying to install it..."
    if command_exists apt-get; then
      run_as_root apt-get update -y
      run_as_root apt-get install -y curl tar
    elif command_exists dnf; then
      run_as_root dnf install -y curl tar
    elif command_exists yum; then
      run_as_root yum install -y curl tar
    elif command_exists apk; then
      run_as_root apk add --no-cache curl tar
    else
      echo "Unable to install curl automatically. Install curl manually and rerun."
      exit 1
    fi
  fi

  install_go_local
  export PATH="$GO_BIN_DIR:$GO_ROOT/bin:$PATH"
}

sync_repo() {
  if [[ -d "$REPO_DIR/.git" ]]; then
    echo "Updating existing repo in $REPO_DIR"
    git -C "$REPO_DIR" fetch --all --prune
    if [[ -n "$REPO_BRANCH" ]]; then
      git -C "$REPO_DIR" checkout "$REPO_BRANCH"
      git -C "$REPO_DIR" pull --ff-only origin "$REPO_BRANCH"
    else
      current_branch="$(git -C "$REPO_DIR" rev-parse --abbrev-ref HEAD)"
      git -C "$REPO_DIR" pull --ff-only origin "$current_branch"
    fi
  else
    rm -rf "$REPO_DIR"
    echo "Cloning $REPO_URL into $REPO_DIR"
    if [[ -n "$REPO_BRANCH" ]]; then
      git clone --depth 1 --branch "$REPO_BRANCH" "$REPO_URL" "$REPO_DIR"
    else
      git clone --depth 1 "$REPO_URL" "$REPO_DIR"
    fi
  fi
}

ensure_run_dir() {
  if [[ ! -f "$RUN_DIR/go.mod" ]] && [[ -z "$APP_SUBDIR" ]]; then
    mapfile -t go_mod_dirs < <(find "$REPO_DIR" -maxdepth 3 -type f -name go.mod -printf '%h\n' | sort -u)
    if [[ "${#go_mod_dirs[@]}" -eq 1 ]]; then
      RUN_DIR="${go_mod_dirs[0]}"
      echo "Auto-detected module root: $RUN_DIR"
    fi
  fi

  if [[ ! -d "$RUN_DIR" ]]; then
    echo "Run directory not found: $RUN_DIR"
    echo "If your Go app is in a subfolder, set APP_SUBDIR at the top of deploy-vps.sh."
    exit 1
  fi
  if [[ ! -f "$RUN_DIR/go.mod" ]]; then
    echo "go.mod not found in $RUN_DIR"
    echo "Set APP_SUBDIR correctly so the script builds the Go module root."
    exit 1
  fi
}

build_binary() {
  echo "Building binary from $RUN_DIR"
  (
    cd "$RUN_DIR"
    go mod download
    go build -trimpath -ldflags="-s -w" -o "$BIN_PATH" .
  )
}

write_run_script() {
  cat >"$RUN_SCRIPT" <<EOF
#!/usr/bin/env bash
set -Eeuo pipefail
export PATH="$GO_BIN_DIR:$GO_ROOT/bin:\$PATH"
mkdir -p "$LOG_DIR"
cd "$RUN_DIR"
ulimit -n 1048576 >/dev/null 2>&1 || true
exec "$BIN_PATH" $SCANNER_ARGS >>"$LOG_FILE" 2>&1
EOF
  chmod +x "$RUN_SCRIPT"
}

stop_nohup_process() {
  if [[ -f "$PID_FILE" ]]; then
    local old_pid
    old_pid="$(cat "$PID_FILE" 2>/dev/null || true)"
    if [[ -n "$old_pid" ]] && kill -0 "$old_pid" 2>/dev/null; then
      kill "$old_pid" 2>/dev/null || true
      sleep 1
    fi
    rm -f "$PID_FILE"
  fi
}

install_systemd_service() {
  local run_user
  run_user="$(id -un)"

  if ! command_exists systemctl; then
    return 1
  fi

  if ! run_as_root test -d /etc/systemd/system; then
    return 1
  fi

  cat <<EOF | run_as_root tee "$SERVICE_FILE" >/dev/null
[Unit]
Description=$APP_NAME
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$run_user
WorkingDirectory=$RUN_DIR
ExecStart=$RUN_SCRIPT
Restart=always
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

  run_as_root systemctl daemon-reload
  run_as_root systemctl enable --now "$APP_NAME"
}

start_nohup() {
  stop_nohup_process
  nohup "$RUN_SCRIPT" >/dev/null 2>&1 &
  echo $! >"$PID_FILE"
  disown || true
}

print_status() {
  echo
  echo "App name : $APP_NAME"
  echo "Repo dir : $REPO_DIR"
  echo "Run dir  : $RUN_DIR"
  echo "Binary   : $BIN_PATH"
  echo "Log file : $LOG_FILE"
  echo

  if command_exists systemctl && [[ -f "$SERVICE_FILE" ]]; then
    echo "Service status:"
    run_as_root systemctl --no-pager --full status "$APP_NAME" || true
  elif [[ -f "$PID_FILE" ]]; then
    echo "PID file : $(cat "$PID_FILE")"
  fi

  echo
  echo "Live log:"
  echo "tail -f $LOG_FILE"
}

main() {
  ensure_repo_url
  ensure_dirs
  ensure_git
  ensure_go
  sync_repo
  ensure_run_dir
  build_binary
  write_run_script

  if install_systemd_service; then
    echo "Started with systemd. The scanner will survive SSH disconnects and reboot."
  else
    start_nohup
    echo "Started with nohup. The scanner will survive SSH disconnects but not a full reboot."
  fi

  print_status
}

main "$@"
