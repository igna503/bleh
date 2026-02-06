# bleh / blehd install notes

## Recommended model

- `bleh` is unprivileged.
- `blehd` runs as **root** (recommended). It owns the BLE device, and exposes a Unix socket for `bleh` to use.
- Access to that socket can be restricted with `--group` (recommended).

## Build

```sh
make build
```

## Install (system)

```sh
sudo make install PREFIX=/usr/local
```

By default (`SETCAP=auto`), `make install` will apply capabilities to `blehd` when run as root.

If you plan to run `blehd` as a **root system service** (recommended), you can skip capabilities:

```sh
sudo make install SETCAP=0
```

## Group access (optional)

If you want group-based access to the socket:

```sh
sudo groupadd -r bleh || true
sudo usermod -aG bleh $USER
# then re-login
```

Then run blehd with `--group bleh`.

## systemd service (recommended)

Install the unit:

```sh
sudo make install-systemd
```

Enable it:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now blehd.service
```

This will run `blehd` as root, listening on:

- `/run/bleh/blehd.sock`

To uninstall:

```sh
sudo make uninstall-systemd
```

## OpenRC service

Install the init script:

```sh
sudo make install-openrc
```

Enable it:

```sh
sudo rc-update add blehd default
sudo rc-service blehd start
```

To uninstall:

```sh
sudo make uninstall-openrc
```
