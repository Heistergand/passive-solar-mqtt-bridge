#!/usr/bin/env bash
set -euo pipefail

APP_NAME="passive-solar-mqtt"
INSTALL_BIN="/usr/local/bin/${APP_NAME}"
CONFIG_DIR="/etc/${APP_NAME}"
CONFIG_FILE="${CONFIG_DIR}/config.yaml"
MAPPING_FILE="${CONFIG_DIR}/homeassistant-mapping.yaml"
PASSWORD_FILE="${CONFIG_DIR}/mqtt-password"
SERVICE_FILE="/etc/systemd/system/${APP_NAME}.service"
INIT=false

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd -- "${SCRIPT_DIR}/.." && pwd)"

die() {
  echo "error: $*" >&2
  exit 1
}

trim_value() {
  local value="$1"

  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

usage() {
  cat <<EOF
Usage: sudo $0 [--init]

Without --init, existing files are reused:
  ${CONFIG_FILE}
  ${PASSWORD_FILE}
  ${MAPPING_FILE}

Only missing values are prompted for.

With --init, existing config, mapping, password, and service files are backed up,
all values are requested again, and a fresh config is written.
EOF
}

parse_args() {
  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --init)
        INIT=true
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        die "unknown argument: $1"
        ;;
    esac
    shift
  done
}

need_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    die "please run as root, for example: sudo $0"
  fi
}

ask() {
  local prompt="$1"
  local default="${2:-}"
  local value
  if [[ -n "${default}" ]]; then
    read -r -p "${prompt} [${default}]: " value
    printf '%s' "${value:-$default}"
  else
    while true; do
      read -r -p "${prompt}: " value
      if [[ -n "${value}" ]]; then
        printf '%s' "${value}"
        return
      fi
      echo "Please enter a value."
    done
  fi
}

ask_password() {
  local first second
  while true; do
    read -r -s -p "MQTT password: " first
    echo
    read -r -s -p "Repeat MQTT password: " second
    echo
    if [[ -n "${first}" && "${first}" == "${second}" ]]; then
      printf '%s' "${first}"
      return
    fi
    echo "Passwords did not match or were empty. Please try again."
  done
}

yaml_get() {
  local section="$1"
  local key="$2"
  local file="$3"

  [[ -f "${file}" ]] || return 1

  awk -v section="${section}" -v key="${key}" '
    /^[^[:space:]#][^:]*:/ {
      current=$1
      sub(/:$/, "", current)
    }
    current == section && $1 == key ":" {
      sub(/^[[:space:]]*[^:]+:[[:space:]]*/, "")
      sub(/[[:space:]]+#.*$/, "")
      gsub(/^[[:space:]]+|[[:space:]]+$/, "")
      gsub(/^["'\''"]|["'\''"]$/, "")
      print
      found=1
      exit
    }
    END { if (!found) exit 1 }
  ' "${file}"
}

service_get() {
  local key="$1"

  [[ -f "${SERVICE_FILE}" ]] || return 1

  awk -F= -v key="${key}" '
    $1 == key {
      print $2
      found=1
      exit
    }
    END { if (!found) exit 1 }
  ' "${SERVICE_FILE}"
}

choose_value() {
  local prompt="$1"
  local existing="$2"
  local fallback="${3:-}"

  if [[ "${INIT}" != "true" && -n "${existing}" ]]; then
    printf '%s' "${existing}"
    return
  fi

  if [[ "${INIT}" == "true" ]]; then
    ask "${prompt}"
  else
    ask "${prompt}" "${fallback}"
  fi
}

interface_exists() {
  local iface
  iface="$(trim_value "$1")"

  [[ -n "${iface}" ]] || return 1
  [[ "${iface}" == "auto" ]] && return 0
  command -v ip >/dev/null 2>&1 || return 0
  ip link show dev "${iface}" >/dev/null 2>&1
}

find_binary() {
  local candidates=(
    "${REPO_DIR}/bin/passive-solar-mqtt-linux-amd64"
    "${REPO_DIR}/passive-solar-mqtt-linux-amd64"
    "${REPO_DIR}/bin/passive-solar-mqtt"
    "${REPO_DIR}/passive-solar-mqtt"
  )
  local candidate
  for candidate in "${candidates[@]}"; do
    if [[ -f "${candidate}" ]]; then
      printf '%s' "${candidate}"
      return
    fi
  done
  die "could not find a built passive-solar-mqtt binary. Build it first or place it next to this script."
}

backup_if_exists() {
  local path="$1"
  if [[ -e "${path}" ]]; then
    cp -a "${path}" "${path}.bak.$(date +%Y%m%d_%H%M%S)"
  fi
}

write_config() {
  local iface="$1"
  local source_ip="$2"
  local cloud_ip="$3"
  local cloud_port="$4"
  local mqtt_broker="$5"
  local mqtt_username="$6"
  local password_file="$7"
  local topic_base="$8"
  local ha_prefix="$9"
  local device_id="${10}"
  local device_name="${11}"
  local mapping_file="${12}"

  cat >"${CONFIG_FILE}" <<EOF
input:
  mode: interface
  interface: ${iface}

simulation:
  realtime: false

capture:
  interface: ${iface}
  source_ip: ${source_ip}
  cloud_ip: ${cloud_ip}
  cloud_port: ${cloud_port}

mqtt:
  enabled: true
  broker: ${mqtt_broker}
  username: ${mqtt_username}
  password_file: ${password_file}
  topic_base: ${topic_base}

homeassistant:
  discovery_prefix: ${ha_prefix}
  device_id: ${device_id}
  device_name: ${device_name}
  mapping_file: ${mapping_file}

logging:
  level: info
EOF

  chmod 0644 "${CONFIG_FILE}"
  chown root:root "${CONFIG_FILE}"
}

write_service() {
  local service_user="$1"
  local service_group="$2"

  cat >"${SERVICE_FILE}" <<EOF
[Unit]
Description=Passive solar MQTT bridge
Documentation=https://github.com/Heistergand/passive-solar-mqtt-bridge
Wants=network-online.target
After=network-online.target
StartLimitIntervalSec=10min
StartLimitBurst=6

[Service]
Type=simple
User=${service_user}
Group=${service_group}
ExecStart=${INSTALL_BIN} --config ${CONFIG_FILE} --mqtt
Restart=on-failure
RestartSec=15s

AmbientCapabilities=CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_RAW
NoNewPrivileges=true

ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true
ProtectClock=true
ProtectControlGroups=true
ProtectHostname=true
ProtectKernelLogs=true
ProtectKernelModules=true
ProtectKernelTunables=true
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK AF_PACKET
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true

LogRateLimitIntervalSec=30s
LogRateLimitBurst=200

[Install]
WantedBy=multi-user.target
EOF
}

main() {
  parse_args "$@"
  need_root

  local default_user="${SUDO_USER:-}"
  if [[ -z "${default_user}" || "${default_user}" == "root" ]]; then
    default_user="$(id -un)"
  fi

  echo "Passive solar MQTT bridge systemd installer"
  echo
  echo "This installs the binary, config, Home Assistant mapping, MQTT password file, and systemd service."
  echo "It does not configure the Linux Layer-2 bridge."
  if [[ "${INIT}" == "true" ]]; then
    echo "Mode: init. Existing files will be backed up and replaced."
  else
    echo "Mode: install/update. Existing config and password files will be reused."
  fi
  echo

  local binary
  binary="$(find_binary)"
  echo "Using binary: ${binary}"
  echo

  local existing_service_user existing_iface existing_source_ip existing_cloud_ip existing_cloud_port existing_mqtt_broker existing_mqtt_username
  local existing_password_file existing_topic_base existing_ha_prefix existing_device_id existing_device_name existing_mapping_file

  if [[ "${INIT}" != "true" && -f "${CONFIG_FILE}" ]]; then
    existing_iface="$(yaml_get input interface "${CONFIG_FILE}" || true)"
    existing_source_ip="$(yaml_get capture source_ip "${CONFIG_FILE}" || true)"
    existing_cloud_ip="$(yaml_get capture cloud_ip "${CONFIG_FILE}" || true)"
    existing_cloud_port="$(yaml_get capture cloud_port "${CONFIG_FILE}" || true)"
    existing_mqtt_broker="$(yaml_get mqtt broker "${CONFIG_FILE}" || true)"
    existing_mqtt_username="$(yaml_get mqtt username "${CONFIG_FILE}" || true)"
    existing_password_file="$(yaml_get mqtt password_file "${CONFIG_FILE}" || true)"
    existing_topic_base="$(yaml_get mqtt topic_base "${CONFIG_FILE}" || true)"
    existing_ha_prefix="$(yaml_get homeassistant discovery_prefix "${CONFIG_FILE}" || true)"
    existing_device_id="$(yaml_get homeassistant device_id "${CONFIG_FILE}" || true)"
    existing_device_name="$(yaml_get homeassistant device_name "${CONFIG_FILE}" || true)"
    existing_mapping_file="$(yaml_get homeassistant mapping_file "${CONFIG_FILE}" || true)"
  fi

  if [[ "${INIT}" != "true" ]]; then
    existing_service_user="$(service_get User || true)"
    if [[ -n "${existing_iface:-}" ]] && ! interface_exists "${existing_iface}"; then
      echo "warning: configured capture interface ${existing_iface} does not exist; using auto-detection"
      existing_iface="auto"
    fi
  fi

  local service_user service_group iface source_ip cloud_ip cloud_port mqtt_broker mqtt_username mqtt_password password_file topic_base ha_prefix device_id device_name mapping_file
  service_user="$(choose_value "Service user" "${existing_service_user:-}" "${default_user}")"
  id "${service_user}" >/dev/null 2>&1 || die "user ${service_user} does not exist"
  service_group="$(id -gn "${service_user}")"

  iface="$(choose_value "Capture interface, for example auto, br0, or the AlphaESS-facing bridge port" "${existing_iface:-}" "auto")"
  source_ip="$(choose_value "AlphaESS source IP" "${existing_source_ip:-}")"
  cloud_ip="$(choose_value "AlphaESS cloud IP" "${existing_cloud_ip:-}")"
  cloud_port="$(choose_value "AlphaESS cloud TCP port" "${existing_cloud_port:-}" "7777")"
  mqtt_broker="$(choose_value "MQTT broker URL" "${existing_mqtt_broker:-}" "tcp://127.0.0.1:1883")"
  mqtt_username="$(choose_value "MQTT username" "${existing_mqtt_username:-}" "alphaess")"
  password_file="${existing_password_file:-${PASSWORD_FILE}}"
  if [[ "${INIT}" == "true" ]]; then
    password_file="${PASSWORD_FILE}"
  fi
  if [[ "${INIT}" == "true" || ! -s "${password_file}" ]]; then
    mqtt_password="$(ask_password)"
  fi
  topic_base="$(choose_value "MQTT topic base" "${existing_topic_base:-}" "alphaess")"
  ha_prefix="$(choose_value "Home Assistant discovery prefix" "${existing_ha_prefix:-}" "homeassistant")"
  device_id="$(choose_value "Home Assistant device ID" "${existing_device_id:-}" "alphaess_t10_hv")"
  device_name="$(choose_value "Home Assistant device name" "${existing_device_name:-}" "AlphaESS SMILE-T10-HV")"
  mapping_file="${existing_mapping_file:-${MAPPING_FILE}}"
  if [[ "${INIT}" == "true" ]]; then
    mapping_file="${MAPPING_FILE}"
  fi

  echo
  echo "Installing..."

  install -m 0755 "${binary}" "${INSTALL_BIN}"
  mkdir -p "${CONFIG_DIR}"

  backup_if_exists "${CONFIG_FILE}"
  backup_if_exists "${MAPPING_FILE}"
  backup_if_exists "${PASSWORD_FILE}"
  backup_if_exists "${SERVICE_FILE}"

  if [[ -f "${REPO_DIR}/configs/homeassistant-mapping.yaml" ]]; then
    install -m 0644 "${REPO_DIR}/configs/homeassistant-mapping.yaml" "${MAPPING_FILE}"
  else
    echo "warning: mapping file template not found; the built-in mapping will be used" >&2
  fi

  if [[ "${INIT}" == "true" || ! -s "${password_file}" ]]; then
    printf '%s\n' "${mqtt_password}" >"${password_file}"
  fi
  chown root:"${service_group}" "${password_file}"
  chmod 0640 "${password_file}"

  write_config "${iface}" "${source_ip}" "${cloud_ip}" "${cloud_port}" "${mqtt_broker}" "${mqtt_username}" "${password_file}" "${topic_base}" "${ha_prefix}" "${device_id}" "${device_name}" "${mapping_file}"
  write_service "${service_user}" "${service_group}"

  systemctl daemon-reload
  systemctl enable "${APP_NAME}.service" >/dev/null
  systemctl reset-failed "${APP_NAME}.service" >/dev/null 2>&1 || true
  systemctl restart "${APP_NAME}.service"

  sleep 2
  if ! systemctl is-active --quiet "${APP_NAME}.service"; then
    echo
    echo "Service did not become active. Recent logs:"
    journalctl -u "${APP_NAME}.service" -n 80 --no-pager || true
    exit 1
  fi

  echo
  echo "Installed and running."
  systemctl --no-pager --full status "${APP_NAME}.service"
}

main "$@"
