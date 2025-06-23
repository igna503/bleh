# Bleh!

![bleh cat](./bleh.jpg)

**Bleh!** is a command-line utility to print images on the MXW01 Bluetooth thermal printer.
It supports 1-bit (1bpp) and 4-bit (4bpp) printing, various dithering algorithms, PNG preview output, and direct communication with the printer via BLE.

## Features

* Print PNG/JPG images (from file or stdin) in 1bpp or 4bpp mode
* Multiple dithering algorithms (Floyd-Steinberg, Bayer, Atkinson, etc.)
* Query printer status, battery, version, and more
* Output a PNG preview instead of printing (for integration or testing)
* Command-line interface with fine-grained options
* Works on Linux using BlueZ (via [go-ble/ble](https://github.com/go-ble/ble))

## Compiling

On a Linux system, run:

```sh
go get bleh
```

Then:

```
go build
```

## Usage

```sh
bleh [options] <image_path or ->
```

### Options

| Option               | Description                                                                         |
| -------------------- | ----------------------------------------------------------------------------------- |
| `-i`, `--intensity`  | Print intensity (0-100) (default: 80)                                               |
| `-m`, `--mode`       | Print mode: 1bpp or 4bpp (default: "1bpp")                                          |
| `-d`, `--dither`     | Dither method: none, floyd, bayer2x2, bayer4x4, bayer8x8, bayer16x16, atkinson, jjn |
| `-s`, `--status`     | Query printer status                                                                |
| `-b`, `--battery`    | Query battery level                                                                 |
| `-v`, `--version`    | Query printer version                                                               |
| `-p`, `--printtype`  | Query print type                                                                    |
| `-q`, `--querycount` | Query internal counter                                                              |
| `-E`, `--eject`      | Eject paper by N lines                                                              |
| `-R`, `--retract`    | Retract paper by N lines                                                            |
| `-o`, `--output`     | Output PNG preview instead of printing. If value is "-", writes PNG to stdout.      |
| `<image_path or ->`  | Path to PNG/JPG image to print, or "-" for stdin                                    |

### Example

```sh
bleh -m 4bpp -d floyd ./myimage.png
```

## Requirements

* Go 1.18+
* BlueZ on Linux (for BLE support)
* Dependencies listed in `go.mod` (see source)

## License

This project is licensed under the terms of the GNU General Public License v3.0 or later.
Parts of the code were ported from [CatPrinterBLE](https://github.com/MaikelChan/CatPrinterBLE) and are licensed under the MIT License.
See the [`LICENSE`](./LICENSE) file for details.

> **Disclaimer:**
> While a license was only added after the project’s initial commits, the current license applies retroactively to all previous commits of this repository.
