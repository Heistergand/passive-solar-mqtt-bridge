#!/usr/bin/env bash
set -euo pipefail

APP_NAME="passive-solar-mqtt"
INSTALL_BIN="/usr/local/bin/${APP_NAME}"
CONFIG_DIR="/etc/${APP_NAME}"
SERVICE_FILE="/etc/systemd/system/${APP_NAME}.service"

die() {
  echo "error: $*" >&2
  exit 1
}

need_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    die "please run as root, for example: sudo $0"
  fi
}

ask_keep_config() {
  local answer

  while true; do
    read -r -p "Keep configuration and secrets in ${CONFIG_DIR}? [yes/no]: " answer
    case "${answer,,}" in
      yes|y|ja|j)
        return 0
        ;;
      no|n|nein)
        return 1
        ;;
      *)
        echo "Please answer yes or no."
        ;;
    esac
  done
}

main() {
  need_root

  echo "Passive solar MQTT bridge uninstaller"
  echo

  local keep_config=false
  if [[ -d "${CONFIG_DIR}" ]]; then
    if ask_keep_config; then
      keep_config=true
    fi
  fi

  if systemctl list-unit-files "${APP_NAME}.service" >/dev/null 2>&1; then
    systemctl disable --now "${APP_NAME}.service" >/dev/null 2>&1 || true
  fi

  rm -f "${SERVICE_FILE}"
  rm -f "${INSTALL_BIN}"
  systemctl daemon-reload
  systemctl reset-failed "${APP_NAME}.service" >/dev/null 2>&1 || true

  if [[ "${keep_config}" == "false" ]]; then
    rm -rf "${CONFIG_DIR}"
  fi

  echo
  echo "Uninstalled ${APP_NAME}."
  if [[ "${keep_config}" == "true" ]]; then
    echo "Configuration kept at ${CONFIG_DIR}."
  else
    echo "Configuration removed."
  fi
  echo "Thanks for giving it a try. The cake was no lie."
}

main "$@"
