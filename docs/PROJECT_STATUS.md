# Project Status

This document captures the project state after the first public release work. It is intentionally short and factual so future work can resume without depending on chat history.

## Current Identity

- Project name: Passive Solar MQTT Bridge
- Runtime binary: `passive-solar-mqtt`
- GitHub repository: `Heistergand/passive-solar-mqtt-bridge`
- Repository visibility: public
- Default branch: `main`
- Version tags: plain semantic versions such as `0.1.0`, without a leading `v`
- License: BSD-2-Clause, copyright holder `passive-solar-mqtt contributors`

The project was originally developed for an AlphaESS SMILE-T10-HV installation, but the public project name deliberately avoids using the vendor or model as the project identity.

## Current Release

Release `0.1.0` was created from tag `0.1.0`.

The GitHub release workflow builds and publishes:

- `passive-solar-mqtt-linux-amd64.tar.gz`
- `passive-solar-mqtt-linux-arm64.tar.gz`
- `checksums.txt`

The `amd64` release archive was downloaded on the target host, verified with `sha256sum -c checksums.txt`, unpacked, and installed successfully with `sudo scripts/install-systemd.sh`.

## Tested Target Layout

Release bundle unpacked on the target:

```text
passive-solar-mqtt/
  bin/
  configs/
  scripts/
  docs/
  README.md
```

Installed runtime layout:

```text
/usr/local/bin/passive-solar-mqtt
/etc/passive-solar-mqtt/config.yaml
/etc/passive-solar-mqtt/homeassistant-mapping.yaml
/etc/passive-solar-mqtt/mqtt-password
/etc/systemd/system/passive-solar-mqtt.service
```

The service was verified as `active (running)` under systemd with auto-detected capture interface logging.

## Current Functionality

- Passive Linux live capture through `AF_PACKET`
- Automatic capture interface detection with `input.interface: auto`
- Offline pcap/pcapng replay
- TCP stream reassembly for the configured AlphaESS-to-cloud flow
- AlphaESS frame envelope validation
- Dynamic JSON field extraction and raw state publishing
- MQTT 3.1.1 publishing
- Home Assistant MQTT Discovery
- Home Assistant Energy Dashboard compatible total energy sensors
- systemd installer and uninstaller
- GitHub Actions CI, Dependabot, govulncheck, and release packaging
- `SECURITY.md` with GitHub Private Vulnerability Reporting guidance
- `LICENSE` with BSD-2-Clause terms

## Current Installer Behavior

Normal installer mode reuses existing config and password files. Missing values are prompted.

If no MQTT password file exists, or if it exists but is empty, the installer asks for a password and writes it with restricted permissions.

If an MQTT password file already exists and is non-empty, the installer treats it as authoritative during installation. After installing and restarting the service, it validates the outcome. If the service fails or the password file looks malformed, the installer reports the problem and offers to re-enter the password.

The installer specifically checks password files for:

- leading blank lines
- trailing blank lines or whitespace
- more than one non-empty line

This behavior is present on `main` after release `0.1.0` and should be included in the next patch release.

## Known Follow-Up

- Create release `0.1.1` for the installer password-file validation fix.
- Consider `CHANGELOG.md` once there is more than one public release.
- Re-read the README from a first-time-user perspective and tighten installation instructions around release downloads.
