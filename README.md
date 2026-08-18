# Passive Solar MQTT Bridge

Passive telemetry reader for MQTT and Home Assistant.

Originally developed for AlphaESS SMILE-T10-HV, this project is for installations where a Linux host can be placed transparently on Layer 2 between an AlphaESS system and the LAN. The host acts as an Ethernet bridge, and this program passively observes the AlphaESS TCP traffic that already crosses that bridge.

The program is intentionally read-only from the AlphaESS network point of view:

- no proxy
- no NAT
- no redirect
- no firewall rule
- no cloud endpoint replacement
- no packets sent to the AlphaESS device or cloud connection

It captures already visible TCP traffic, reassembles the TCP byte stream, validates AlphaESS frames, parses JSON payloads, and publishes selected values to MQTT and Home Assistant.

If you are looking for a cloud API client or an active inverter control integration, this is not that. If you can physically place a small Linux box with two Ethernet ports between AlphaESS and the LAN, and you want a passive listener that leaves the original cloud connection alone, you are in the right place.

## Network Assumption

The recommended topology is a transparent Linux bridge:

```text
AlphaESS system
      |
alphaess-facing NIC
      |
Linux host with bridge br0
      |
LAN-facing NIC
      |
LAN / router / internet / AlphaESS cloud
```

The Linux host is physically in the path, but it does not become an IP proxy for AlphaESS. The AlphaESS system remains in the normal LAN and keeps talking to the AlphaESS cloud directly. The program listens on the bridge or one of its ports and publishes observed telemetry to MQTT.

Operationally important distinction:

- If `passive-solar-mqtt` stops, AlphaESS traffic should continue through the Linux bridge.
- If the Linux host, bridge, or cabling fails, the physical connection is interrupted.

Use this project only if that network model is acceptable for your installation.

## Linux Layer-2 Bridge

This section is a starting point for a small Ubuntu or Debian-style host with two Ethernet interfaces. Interface names differ between systems; replace `enp1s0` and `enp2s0` with your actual names from:

```bash
ip link
```

Example role assignment:

```text
enp1s0  connected to AlphaESS
enp2s0  connected to LAN/router/switch
br0     Linux bridge, optional management IP for the host
```

With netplan, a DHCP-managed bridge can look like this:

```yaml
network:
  version: 2
  renderer: networkd
  ethernets:
    enp1s0:
      dhcp4: false
    enp2s0:
      dhcp4: false
  bridges:
    br0:
      interfaces:
        - enp1s0
        - enp2s0
      dhcp4: true
      parameters:
        stp: false
        forward-delay: 0
```

With a static management address on the Linux host:

```yaml
network:
  version: 2
  renderer: networkd
  ethernets:
    enp1s0:
      dhcp4: false
    enp2s0:
      dhcp4: false
  bridges:
    br0:
      interfaces:
        - enp1s0
        - enp2s0
      addresses:
        - 192.0.2.20/24
      routes:
        - to: default
          via: 192.0.2.1
      nameservers:
        addresses:
          - 192.0.2.1
      parameters:
        stp: false
        forward-delay: 0
```

### Stable Interface Names

For a long-running bridge host, prefer stable names based on MAC addresses. This is especially useful for USB Ethernet adapters, because their kernel-generated names can change when hardware is replaced.

Inspect the current interfaces:

```bash
ip -br link
ip -br addr
bridge link
```

For a USB Ethernet adapter, inspect vendor, model, and serial metadata:

```bash
udevadm info -q property -p /sys/class/net/<interface> | grep -E 'ID_MODEL|ID_VENDOR|ID_SERIAL|INTERFACE'
```

Example netplan configuration with a built-in LAN port and a USB-to-RJ45 adapter renamed to `en_usb2rj45`:

```yaml
network:
  version: 2
  renderer: networkd

  ethernets:
    enp3s0:
      dhcp4: false
      dhcp6: false

    en_usb2rj45:
      match:
        macaddress: 9c:69:d3:a0:c5:c4
      set-name: en_usb2rj45
      dhcp4: false
      dhcp6: false

  bridges:
    br0:
      interfaces:
        - enp3s0
        - en_usb2rj45
      macaddress: 68:84:7e:a4:a1:3f
      dhcp4: true
      dhcp6: true
      dhcp-identifier: mac
```

When replacing the USB adapter, update the `macaddress` and keep the semantic name if the role stays the same. Then apply with `netplan try` and verify that both physical ports are bridge members:

```bash
sudo netplan try
bridge link
```

Expected bridge membership:

```text
enp3s0 master br0
en_usb2rj45 master br0
```

Apply carefully, preferably with `netplan try` when you are working over SSH:

```bash
sudo netplan try
sudo netplan apply
bridge link
ip addr show br0
```

Before installing the service, verify where AlphaESS traffic is visible. Depending on the kernel, driver, and bridge setup, you may see the packets on `br0` or on the AlphaESS-facing bridge port:

```bash
sudo tcpdump -i br0 host <alphaess-ip> and tcp port 7777
sudo tcpdump -i enp1s0 host <alphaess-ip> and tcp port 7777
sudo tcpdump -i any host <alphaess-ip> and tcp port 7777
```

Use `auto` as `input.interface` to let the service detect the interface from the configured AlphaESS TCP flow. If you prefer a fixed setup, use the interface that actually sees the AlphaESS packets as `input.interface` in the config or when the installer asks for the capture interface.

With `auto`, the service logs the selected interface:

```text
auto-detected capture interface: en_usb2rj45
```

If the service reports `no AlphaESS TCP traffic seen within 2m0s on active interfaces`, first check `bridge link` and `tcpdump -i any ...`. The most common cause is that one physical port is up but not attached to `br0`.

## Implementation Status

Current public status:

- repository: public GitHub project at `Heistergand/passive-solar-mqtt-bridge`
- current release: `0.1.0`
- license: BSD-2-Clause
- release artifacts: Linux `amd64` and `arm64` tarballs plus `checksums.txt`
- tested target path: `/opt/passive-solar-mqtt` unpacked release bundle, installing to `/usr/local/bin/passive-solar-mqtt` and `/etc/passive-solar-mqtt`
- current development branch includes an installer fix planned for `0.1.1`: existing MQTT password files are respected first, then checked after installation; malformed password files can be replaced interactively

The implemented passive pipeline is:

```text
config -> pcap/live capture -> TCP reassembly -> AlphaESS frame validation -> JSON extraction -> MQTT -> Home Assistant discovery
```

Implemented pieces:

1. pcap/pcapng replay for offline analysis
2. Linux live capture with a passive `AF_PACKET` raw socket
3. outbound AlphaESS TCP stream reassembly
4. AlphaESS frame length and CRC16-Modbus validation
5. dynamic JSON extraction and normalized state payloads
6. MQTT 3.1.1 publishing
7. Home Assistant MQTT Discovery for a conservative base sensor set
8. retained availability status with Last Will support

## Usage

The program looks for a configuration file in these locations:

```text
$PASSIVE_SOLAR_MQTT_CONFIG
$XDG_CONFIG_HOME/passive-solar-mqtt/config.yaml
$HOME/.config/passive-solar-mqtt/config.yaml
/etc/passive-solar-mqtt/config.yaml
```

Or you can pass it explicitly:

```bash
passive-solar-mqtt --config configs/example.yaml
passive-solar-mqtt -c configs/example.yaml
```

MQTT publishing is disabled by default for safe offline analysis. Enable it explicitly when the broker target is correct:

```bash
passive-solar-mqtt --config configs/example.yaml --mqtt
```

or in the config:

```yaml
mqtt:
  enabled: true
```

MQTT authentication is optional. If your broker requires a dedicated user, set both fields:

```yaml
mqtt:
  username: alphaess
  password_file: /run/secrets/alphaess_mqtt_password
```

The password file should contain only the password and must not be committed.

If no config file is found, or if the config is incomplete, the program asks for the missing values one by one. For the input source, enter `auto`, an interface name, or a capture file:

```text
auto
enx0c3796bef0d8
pcaps/sample.pcapng
```

If the value looks like a `.pcap` or `.pcapng` file, it is stored as `input.mode: file`; otherwise it is stored as `input.mode: interface`. With `auto`, Linux live capture listens briefly across active non-loopback interfaces, chooses the interface where the configured AlphaESS TCP flow appears, and then continues with the normal bound capture.

After setup, the program asks whether the completed config should be saved:

```text
ja        save the config and continue
nein      continue without saving
abbrechen stop without running
```

When an existing config was loaded, `ja` updates that file. When no config existed, `ja` writes a new config file to the selected Linux-style config path.

Read from a saved capture file:

```bash
passive-solar-mqtt --config configs/example.yaml --file pcaps/sample.pcapng
passive-solar-mqtt -c configs/example.yaml -f pcaps/sample.pcapng
```

Replay a saved capture with its original packet timing:

```bash
passive-solar-mqtt --config configs/example.yaml --file pcaps/sample.pcapng --realtime
```

Without `--realtime`, pcap files are read as fast as possible. `--realtime` is only valid for file input.

Enable verbose output:

```bash
passive-solar-mqtt --config configs/example.yaml --verbose
passive-solar-mqtt -c configs/example.yaml -v
```

Verbose mode reports packet, stream, JSON, discovery, availability, and MQTT publish activity.
During live capture it also prints periodic heartbeat lines with packet, matched-flow, JSON, and MQTT counters.
For file input, the program reads the pcap/pcapng file, reconstructs the AlphaESS-to-cloud TCP stream, extracts JSON messages, and builds or publishes raw MQTT state payloads. With `--mqtt`, the same pipeline publishes to the configured broker.

The current raw state topic is:

```text
alphaess/raw/state
```

The payload keeps the original AlphaESS fields and adds normalized numeric values plus simple derived values such as `PpvTotal`, `PrealTotal`, and `PmeterTotal` when numbered phase or PV input fields are present. State messages are retained so Home Assistant can read the latest known value immediately after MQTT Discovery creates the entities; availability still comes from `alphaess/status`.

The program also builds Home Assistant MQTT Discovery payloads. By default it uses a safe built-in base sensor set. To customize or replace the Home Assistant mapping without rebuilding the binary, point the config to a YAML mapping file:

```yaml
homeassistant:
  mapping_file: /etc/passive-solar-mqtt/homeassistant-mapping.yaml
```

Relative mapping paths are resolved relative to the config file. See `configs/homeassistant-mapping.yaml` for an inline-documented example. Keep unknown fields as diagnostic sensors or leave them out; the full raw JSON remains available on `alphaess/raw/state`.

The built-in base mapping contains:

```text
SOC
Pbat
PpvTotal
Ppv1..Ppv4
EpvTotal
PrealL1..PrealL3
PrealTotal
PmeterL1..PmeterL3
PmeterTotal
Einput
Eoutput
Echarge
EDischarge
```

Discovery messages are retained and use `alphaess/raw/state` as the state topic.
They also use `alphaess/status` as the availability topic. The program publishes retained `online` status when the pipeline starts. With MQTT publishing enabled, the broker receives a Last Will that can mark the device `offline` if the process disappears unexpectedly; on a controlled live-capture shutdown, the program publishes retained `offline` before disconnecting.

The `EpvTotal`, `Einput`, `Eoutput`, `Echarge`, and `EDischarge` sensors are published as Home Assistant energy sensors with `kWh` and `total_increasing` state class. They are intended for Home Assistant's Energy Dashboard. Live power sensors such as `PV Power Total` and `Grid Power Total` remain `W` measurement sensors and can be used only in the wizard's optional power fields.

If the MQTT connection breaks during a publish, the client closes the stale socket, reconnects once, and retries that publish. If reconnecting also fails, the process exits with an error; the example systemd unit restarts it without affecting the AlphaESS cloud connection.

Read from a live interface or let the program detect it:

```bash
passive-solar-mqtt --config configs/example.yaml --interface auto
passive-solar-mqtt --config configs/example.yaml --interface enx0c3796bef0d8
passive-solar-mqtt -c configs/example.yaml -i enx0c3796bef0d8
```

If neither `--file` nor `--interface` is provided, the input from the config file is used. `--file` and `--interface` are mutually exclusive. If a configured fixed interface no longer exists, for example after replacing a USB Ethernet adapter, the service attempts the same auto-detection before failing.

Live capture is implemented for Linux using a passive `AF_PACKET` raw socket. It does not change bridge, routing, firewall, NAT, or proxy settings. The binary needs permission to open the packet socket, for example through `CAP_NET_RAW`:

```bash
sudo setcap cap_net_raw=eip /usr/local/bin/passive-solar-mqtt
```

For a first live test without publishing to MQTT:

```bash
passive-solar-mqtt --config /etc/passive-solar-mqtt/config.yaml --interface auto --verbose --no-mqtt
```

For MQTT publishing:

```bash
passive-solar-mqtt --config /etc/passive-solar-mqtt/config.yaml --interface auto --mqtt
```

## Systemd Installation

Build the Linux binary:

```bash
GOOS=linux GOARCH=amd64 go build -o bin/passive-solar-mqtt-linux-amd64 ./cmd/passive-solar-mqtt
```

If you used a pre-release installation named `alphaess-passive`, stop and disable that old service before installing the renamed runtime. Otherwise both services may capture and publish to MQTT at the same time:

```bash
sudo systemctl disable --now alphaess-passive.service
```

Copy the repository folder or at least the binary, `configs/`, and `scripts/` directory to the target Linux host. The easiest installation path is the interactive installer:

```bash
sudo scripts/install-systemd.sh
```

Run the installer again after copying a newer binary or updated config templates. In normal mode it reuses an existing `/etc/passive-solar-mqtt/config.yaml` and MQTT password file, and only asks for values that are missing.

To intentionally create a fresh configuration, use:

```bash
sudo scripts/install-systemd.sh --init
```

`--init` backs up existing config, mapping, password, and service files with a timestamp suffix before writing the new files.

The installer:

- installs the binary to `/usr/local/bin/passive-solar-mqtt`
- creates or updates `/etc/passive-solar-mqtt/config.yaml`
- creates or updates `/etc/passive-solar-mqtt/homeassistant-mapping.yaml`
- asks for the MQTT password without echoing it only when no password file exists or `--init` is used
- treats an existing non-empty MQTT password file as authoritative during installation
- validates the service after installation and offers to re-enter the MQTT password if the service fails or if the password file contains suspicious whitespace or multiple non-empty lines
- stores the MQTT password file with restricted permissions
- creates or updates `/etc/systemd/system/passive-solar-mqtt.service`
- enables and starts the service
- fails with recent journal output if the service is not actually active

The installer does not configure the Linux Layer-2 bridge. Build and verify the bridge first, then pass the interface that sees AlphaESS packets when the installer asks for the capture interface.

For manual installation:

```bash
sudo install -m 0755 bin/passive-solar-mqtt-linux-amd64 /usr/local/bin/passive-solar-mqtt
sudo mkdir -p /etc/passive-solar-mqtt
sudo install -m 0644 configs/example.yaml /etc/passive-solar-mqtt/config.yaml
sudo install -m 0644 configs/homeassistant-mapping.yaml /etc/passive-solar-mqtt/homeassistant-mapping.yaml
sudo editor /etc/passive-solar-mqtt/config.yaml
```

Set the mapping file in `/etc/passive-solar-mqtt/config.yaml` if you want to manage Home Assistant entities outside the binary:

```yaml
homeassistant:
  mapping_file: /etc/passive-solar-mqtt/homeassistant-mapping.yaml
```

For a systemd service, copy `configs/passive-solar-mqtt.service.example` to `/etc/systemd/system/passive-solar-mqtt.service` and adjust `User=`/`Group=` if needed. The unit grants only `CAP_NET_RAW` to the service so the passive Linux packet socket can be opened without running the whole process as root.

```bash
sudo install -m 0644 configs/passive-solar-mqtt.service.example /etc/systemd/system/passive-solar-mqtt.service
sudo systemctl daemon-reload
sudo systemctl enable --now passive-solar-mqtt.service
sudo systemctl is-active --quiet passive-solar-mqtt.service
sudo journalctl -u passive-solar-mqtt.service -f
```

Check the systemd hardening score:

```bash
systemd-analyze security passive-solar-mqtt.service
```

The service logs to journald. A classic logrotate file is not needed unless you redirect logs to files. Limit journal growth on small hosts with `/etc/systemd/journald.conf` or a drop-in:

```ini
[Journal]
SystemMaxUse=200M
MaxRetentionSec=14day
```

Then restart journald:

```bash
sudo systemctl restart systemd-journald.service
```

For a temporary more talkative service, create an override:

```bash
sudo systemctl edit passive-solar-mqtt.service
```

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/passive-solar-mqtt --config /etc/passive-solar-mqtt/config.yaml --mqtt --verbose
```

Reload and restart:

```bash
sudo systemctl daemon-reload
sudo systemctl restart passive-solar-mqtt.service
```

Remove the override again when verbose packet logging is no longer needed:

```bash
sudo systemctl revert passive-solar-mqtt.service
sudo systemctl restart passive-solar-mqtt.service
```

## Uninstall

To remove the installed service and binary:

```bash
sudo scripts/uninstall.sh
```

The uninstaller asks whether `/etc/passive-solar-mqtt` should be kept. Keep it if you plan to reinstall later with the same MQTT credentials and Home Assistant mapping. Remove it if you want to delete local configuration and secrets as well.
