# Architecture

## Non-Negotiable Rule

The service must be unable by design to affect the AlphaESS cloud connection.

Failure mode must be:

```text
no fresh MQTT values
```

Never:

```text
AlphaESS loses cloud connectivity
```

## Packages

```text
cmd/alphaess-passive/   executable entry point
internal/config/        configuration loading and defaults
internal/capture/       passive packet capture
internal/reassembly/    TCP stream reconstruction
internal/alphaess/      AlphaESS frame and JSON decoding
internal/mqtt/          MQTT publishing
internal/homeassistant/ Home Assistant discovery payloads
```

## Data Flow

```text
pcap packets
    |
TCP segments
    |
reassembled byte stream
    |
AlphaESS frames with length and CRC16-Modbus validation
    |
JSON maps
    |
MQTT state topics
    |
Home Assistant sensors
```

## Current Runtime Behavior

The executable can read either saved pcap/pcapng files or a live Linux interface. Live capture uses a passive `AF_PACKET` raw socket and does not alter bridge, routing, NAT, firewall, proxy, or AlphaESS traffic.

MQTT publishing is disabled by default. When enabled, the program publishes retained Home Assistant discovery messages, retained availability status on `alphaess/status`, and raw state JSON on `alphaess/raw/state`. The MQTT client also sets a retained Last Will so Home Assistant can mark the device unavailable if the service disappears unexpectedly.

The AlphaESS parser validates the observed frame envelope before decoding JSON. The frame starts with `01 01`, uses a 7-byte header, stores the JSON payload length as a big-endian `uint32` in header bytes 3..6, and ends with a CRC16-Modbus checksum over header plus payload. The checksum bytes are stored big-endian in the stream.

After frame validation, parsing stays intentionally dynamic: it preserves all raw fields, normalizes numeric values, and derives `PpvTotal` from present `PpvN` fields. Device-specific profiles should only be added if real captures prove that the dynamic parser is insufficient.
