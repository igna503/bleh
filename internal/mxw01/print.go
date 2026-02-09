package mxw01

import (
	"fmt"
	"time"

	ble "github.com/go-ble/ble"
)

func SendImageBufferToPrinter(client ble.Client, dataChr, printChr *ble.Characteristic, pixels []byte, height int, mode PrintMode, intensity byte, attMTU int) error {
	cmd := BuildCommand(0xA2, []byte{intensity})
	if err := client.WriteCharacteristic(printChr, cmd, true); err != nil {
		return fmt.Errorf("intensity set failed: %v", err)
	}

	param := []byte{byte(height & 0xFF), byte(height >> 8), 0x30, byte(mode)}
	cmd = BuildCommand(0xA9, param)
	if err := client.WriteCharacteristic(printChr, cmd, true); err != nil {
		return fmt.Errorf("print command failed: %v", err)
	}

	bytesPerLine := LinePixels / 8
	if mode == Mode4bpp {
		bytesPerLine = LinePixels / 2
	}

	// Keep data writes conservative for stability across adapters/firmware.
	payloadMTU := 20
	for y := 0; y < height; y++ {
		slice := pixels[y*bytesPerLine : (y+1)*bytesPerLine]
		for offset := 0; offset < len(slice); offset += payloadMTU {
			end := offset + payloadMTU
			if end > len(slice) {
				end = len(slice)
			}
			chunk := slice[offset:end]
			if err := client.WriteCharacteristic(dataChr, chunk, true); err != nil {
				return fmt.Errorf("line %d chunk write failed: %v", y, err)
			}
			time.Sleep(DefaultSleep)
		}
	}

	cmd = BuildCommand(0xAD, []byte{0x00})
	if err := client.WriteCharacteristic(printChr, cmd, true); err != nil {
		return fmt.Errorf("flush failed: %v", err)
	}
	return nil
}
