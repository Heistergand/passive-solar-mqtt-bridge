# Security Policy

## Supported Versions

Security fixes are provided for the latest released version and current `main`.

This project is young. Until a stable `1.0.0` release exists, security-relevant fixes may be released as patch versions without long-term maintenance branches.

## Reporting a Vulnerability

Please do not open a public issue for a vulnerability that could help someone attack a running installation.

Use GitHub Private Vulnerability Reporting for this repository.

Open the repository on GitHub, go to **Security and quality**, then choose **Report a vulnerability**.

Include:

- affected version or commit
- operating system and installation mode
- a short description of the impact
- reproduction steps, if safe to share
- whether credentials, packet captures, or private network details are involved

Do not send real MQTT passwords, private keys, or packet captures unless explicitly requested and agreed upon.

## Project Security Scope

`passive-solar-mqtt` is designed as a passive listener. It must not intentionally send packets to the observed solar system, proxy its cloud connection, rewrite traffic, or provide a control path.

Security-sensitive areas include:

- packet capture permissions and `CAP_NET_RAW`
- systemd unit hardening
- MQTT credentials and password file handling
- Home Assistant MQTT Discovery payloads
- release artifacts and checksums
- accidental inclusion of packet captures, secrets, or local environment details

## Operational Recommendations

- Run the service as an unprivileged user.
- Grant only the capability needed for passive capture.
- Keep MQTT credentials in a file with restricted permissions.
- Do not commit `pcaps/`, binaries, caches, local configs, or secrets.
- Verify release downloads with `sha256sum -c checksums.txt`.
- Keep the Linux bridge configuration simple and observable.

## Non-Goals

This project is not an inverter control interface, not a proxy, and not a replacement cloud endpoint. Any change that adds active control or traffic modification must be treated as a separate security design.
