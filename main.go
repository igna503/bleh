/*
This file is part of Bleh!.

Bleh! is free software: you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version.

Bleh! is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for more details.

You should have received a copy of the GNU General Public License along with Foobar. If not, see <https://www.gnu.org/licenses/>.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/disintegration/imaging"
	ble "github.com/go-ble/ble"
	"github.com/go-ble/ble/linux"
	dither "github.com/makeworld-the-better-one/dither"
)

const minLines = 86 // firmware refuses to print anything shorter

var (
	mainServiceUUID      = ble.MustParse("ae30")
	printCharacteristic  = ble.MustParse("ae01")
	notifyCharacteristic = ble.MustParse("ae02")
	dataCharacteristic   = ble.MustParse("ae03")
	targetPrinterName    = "MXW01"
	scanTimeout          = 10 * time.Second
	printCommandHeader   = []byte{0x22, 0x21}
	printCommandFooter   = byte(0xFF)
	intensity            int
	mode                 string
	ditherType           string
	getStatus            bool
	getBattery           bool
	getVersion           bool
	getPrintType         bool
	getQueryCount        bool
	ejectPaper           uint
	retractPaper         uint
	outputPath           string
	address              string
	version              = "dev"
)

func init() {
	flag.IntVar(&intensity, "intensity", 80, "Print intensity (0-100)")
	flag.IntVar(&intensity, "i", 80, "Print intensity (0-100)")

	flag.StringVar(&mode, "mode", "1bpp", "Print mode: 1bpp or 4bpp")
	flag.StringVar(&mode, "m", "1bpp", "Print mode: 1bpp or 4bpp")

	flag.StringVar(&ditherType, "dither", "none", "Dither method: none, floyd, bayer2x2, bayer4x4, bayer8x8, bayer16x16, atkinson, jjn")
	flag.StringVar(&ditherType, "d", "none", "Dither method: none, floyd, bayer2x2, bayer4x4, bayer8x8, bayer16x16, atkinson, jjn")

	flag.BoolVar(&getStatus, "status", false, "Query printer status")
	flag.BoolVar(&getStatus, "s", false, "Query printer status")

	flag.BoolVar(&getBattery, "battery", false, "Query battery level")
	flag.BoolVar(&getBattery, "b", false, "Query battery level")

	flag.BoolVar(&getVersion, "version", false, "Query printer version")
	flag.BoolVar(&getVersion, "v", false, "Query printer version")

	flag.BoolVar(&getPrintType, "printtype", false, "Query print type")
	flag.BoolVar(&getPrintType, "p", false, "Query print type")

	flag.BoolVar(&getQueryCount, "querycount", false, "Query internal counter")
	flag.BoolVar(&getQueryCount, "q", false, "Query internal counter")

	flag.UintVar(&ejectPaper, "eject", 0, "Eject paper by N lines")
	flag.UintVar(&ejectPaper, "E", 0, "Eject paper by N lines")

	flag.UintVar(&retractPaper, "retract", 0, "Retract paper by N lines")
	flag.UintVar(&retractPaper, "R", 0, "Retract paper by N lines")

	flag.StringVar(&outputPath, "o", "", "Output PNG preview instead of printing (specify output path)")
	flag.StringVar(&outputPath, "output", "", "Output PNG preview instead of printing (specify output path)")

	flag.StringVar(&address, "a", "", "Connect to printer by MAC address")
	flag.StringVar(&address, "address", "", "Connect to printer by MAC address")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Bleh! Cat Printer Utility for MXW01, version %s\n", version)
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <image_path or ->\n", os.Args[0])
		fmt.Fprintln(os.Stderr, `
Options:
  -h, --help               Show this help message
  -a, --address <mac>      Connect to printer by MAC address
  -i, --intensity int      Print intensity (0-100) (default 80)
  -m, --mode string        Print mode: 1bpp or 4bpp (default "1bpp")
  -d, --dither string      Dither method: none, floyd, bayer2x2, bayer4x4, bayer8x8, bayer16x16, atkinson, jjn (default "none")
  -s, --status             Query printer status
  -b, --battery            Query battery level
  -v, --version            Query printer version
  -p, --printtype          Query print type
  -q, --querycount         Query internal counter
  -E, --eject uint         Eject paper by N lines
  -R, --retract uint       Retract paper by N lines
  -o, --output <file>      Output PNG preview instead of printing.
                           If <file> is "-", writes PNG to stdout.
  <image_path or ->        Path to PNG/JPG to print, or '-' for stdin`)
	}
}

func parseNotification(data []byte) {
	if len(data) < 2 || data[0] != 0x22 || data[1] != 0x21 {
		fmt.Printf("Invalid notification header, raw: % X", data)
		return
	}

	cmd := data[2]
	dataLen := int(data[4]) | int(data[5])<<8

	switch cmd {
	case 0xA1: // GetStatus
		battery := data[9]
		temp := data[10]
		statusOk := data[12] == 0
		statusCode := data[6]
		errCode := data[13]

		statusMsg := "Unknown"
		if statusOk {
			switch statusCode {
			case 0x0:
				statusMsg = "Standby"
			case 0x1:
				statusMsg = "Printing"
			case 0x2:
				statusMsg = "Feeding paper"
			case 0x3:
				statusMsg = "Ejecting paper"
			}
		} else {
			switch errCode {
			case 0x1, 0x9:
				statusMsg = "No paper"
			case 0x4:
				statusMsg = "Overheated"
			case 0x8:
				statusMsg = "Low battery"
			}
		}

		fmt.Printf("Status: %v (%s), Battery: %d, Temp: %d\n", statusOk, statusMsg, battery, temp)

	case 0xA3: // EjectPaper
		fmt.Println("Ejecting paper...")

	case 0xA4: // RetractPaper
		fmt.Println("Retracting paper...")

	case 0xA7: // QueryCount
		if len(data) >= 12 {
			fmt.Printf("Query count: % X\n", data[6:12])
		}

	case 0xA9: // Print
		printOk := data[6] == 0
		fmt.Printf("Print status: %s\n", map[bool]string{true: "Ok", false: "Failure"}[printOk])

	case 0xAA: // PrintComplete
		fmt.Println("Printing finished.")

	case 0xAB: // BatteryLevel
		fmt.Printf("Battery level: %d\n", data[6])

	case 0xB0: // GetPrintType
		var t string
		switch data[6] {
		case 0x01:
			t = `High pressure`
		case 0xFF:
			t = `"Unknown`
		default:
			t = `Low pressure`
		}
		fmt.Printf("Print type: %s\n", t)

	case 0xB1: // GetVersion
		if len(data) < 14+dataLen {
			fmt.Println("Malformed version notification")
			return
		}
		version := string(data[6 : 6+dataLen])
		var t string
		switch data[14] {
		case 0x32:
			t = `High pressure`
		case 0x31:
			t = `Low pressure`
		default:
			t = `Unknown`
		}
		fmt.Printf("Version: %s, Print type: %s\n", version, t)

	default:
		fmt.Printf("Received notification for unknown command: 0x%02X\n", cmd)
	}
}

const (
	linePixels   = 384
	bytesPerLine = linePixels / 8
)

// decodeImage loads an image from a given path or stdin ("-")
func decodeImage(path string) (image.Image, error) {
	if path == "-" {
		return decodeImageFromReader(os.Stdin)
	}
	img, err := imaging.Open(path, imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("failed to open image %q: %v", path, err)
	}
	return img, nil
}

// decodeImageFromReader reads and decodes an image from any io.Reader
func decodeImageFromReader(r io.Reader) (image.Image, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}
	return img, nil
}

// loadImageMonoFromImage processes an image.Image to 1bpp packed byte format
func loadImageMonoFromImage(img image.Image, ditherType string) ([]byte, int, error) {
	ratio := float64(img.Bounds().Dx()) / float64(img.Bounds().Dy())
	height := int(float64(linePixels) / ratio)
	img = imaging.Resize(img, linePixels, height, imaging.Lanczos)
	img = imaging.Grayscale(img)

	if ditherType != "none" {
		palette := []color.Color{color.Black, color.White}
		d := dither.NewDitherer(palette)
		switch ditherType {
		case "floyd":
			d.Matrix = dither.FloydSteinberg
		case "bayer2x2":
			d.Mapper = dither.Bayer(2, 2, 1.0)
		case "bayer4x4":
			d.Mapper = dither.Bayer(4, 4, 1.0)
		case "bayer8x8":
			d.Mapper = dither.Bayer(8, 8, 1.0)
		case "bayer16x16":
			d.Mapper = dither.Bayer(16, 16, 1.0)
		case "atkinson":
			d.Matrix = dither.Atkinson
		case "jjn":
			d.Matrix = dither.JarvisJudiceNinke
		default:
			return nil, 0, fmt.Errorf("unknown dither type: %s", ditherType)
		}
		img = d.DitherCopy(img)
	} else {
		img = imaging.AdjustContrast(img, 10)
	}

	pixels := make([]byte, (linePixels*height)/8)
	for y := 0; y < height; y++ {
		for x := 0; x < linePixels; x++ {
			gray := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			if gray.Y < 128 {
				idx := (y*linePixels + x) / 8
				pixels[idx] |= 1 << (x % 8)
			}
		}
	}

	return pixels, height, nil
}

// loadImage4BitFromImage processes an image.Image to 4bpp packed byte format
func loadImage4BitFromImage(img image.Image, ditherType string) ([]byte, int, error) {
	ratio := float64(img.Bounds().Dx()) / float64(img.Bounds().Dy())
	height := int(float64(linePixels) / ratio)
	img = imaging.Resize(img, linePixels, height, imaging.Lanczos)
	img = imaging.Grayscale(img)

	palette := make([]color.Color, 16)
	for i := 0; i < 16; i++ {
		v := uint8(i * 17)
		palette[i] = color.Gray{Y: 255 - v}
	}

	if ditherType != "none" {
		d := dither.NewDitherer(palette)
		switch ditherType {
		case "floyd":
			d.Matrix = dither.FloydSteinberg
		case "bayer2x2":
			d.Mapper = dither.Bayer(2, 2, 0.2)
		case "bayer4x4":
			d.Mapper = dither.Bayer(4, 4, 0.2)
		case "bayer8x8":
			d.Mapper = dither.Bayer(8, 8, 0.2)
		case "bayer16x16":
			d.Mapper = dither.Bayer(16, 16, 0.2)
		case "atkinson":
			d.Matrix = dither.Atkinson
		case "jjn":
			d.Matrix = dither.JarvisJudiceNinke
		default:
			return nil, 0, fmt.Errorf("unknown dither type: %s", ditherType)
		}
		img = d.DitherCopy(img)
	}

	width := linePixels
	pixels := make([]byte, (width*height)/2)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			gray := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			level := (255 - gray.Y) >> 4 // 0..15, inverted logic
			idx := (y*width + x) >> 1
			shift := uint(((x & 1) ^ 1) << 2)
			pixels[idx] |= level << shift
		}
	}
	return pixels, height, nil
}

// Extend sendImageToPrinter to handle 4-bit mode
type PrintMode byte

const (
	Mode1bpp PrintMode = 0x00
	Mode4bpp PrintMode = 0x02
)

func sendImageBufferToPrinter(client ble.Client, dataChr, printChr *ble.Characteristic, pixels []byte, height int, mode PrintMode, intensity byte) error {
	fmt.Printf("Sending image: %dx%d lines\n", linePixels, height)

	cmd := buildCommand(0xA2, []byte{intensity})
	if err := client.WriteCharacteristic(printChr, cmd, true); err != nil {
		return fmt.Errorf("intensity set failed: %v", err)
	}

	param := []byte{
		byte(height & 0xFF), byte(height >> 8),
		0x30,
		byte(mode),
	}
	cmd = buildCommand(0xA9, param)
	if err := client.WriteCharacteristic(printChr, cmd, true); err != nil {
		return fmt.Errorf("print command failed: %v", err)
	}

	bytesPerLine := linePixels / 8
	if mode == Mode4bpp {
		bytesPerLine = linePixels / 2
	}

	mtu := 20
	for y := 0; y < height; y++ {
		slice := pixels[y*bytesPerLine : (y+1)*bytesPerLine]
		for offset := 0; offset < len(slice); offset += mtu {
			end := offset + mtu
			if end > len(slice) {
				end = len(slice)
			}
			chunk := slice[offset:end]
			if err := client.WriteCharacteristic(dataChr, chunk, true); err != nil {
				return fmt.Errorf("line %d chunk write failed: %v", y, err)
			}
			time.Sleep(6 * time.Millisecond)
		}
	}

	cmd = buildCommand(0xAD, []byte{0x00})
	if err := client.WriteCharacteristic(printChr, cmd, true); err != nil {
		return fmt.Errorf("flush failed: %v", err)
	}

	return nil
}

func padImageToMinLines(img image.Image, minLines int) image.Image {
	bounds := img.Bounds()
	if bounds.Dy() >= minLines {
		return img
	}
	// Create a new white image
	dst := imaging.New(bounds.Dx(), minLines, color.White)
	// Paste the original image at the top
	dst = imaging.Paste(dst, img, image.Pt(0, 0))
	return dst
}

func sendSimpleCommand(client ble.Client, printChr *ble.Characteristic, cmdId byte) error {
	cmd := buildCommand(cmdId, []byte{0x00})
	return client.WriteCharacteristic(printChr, cmd, true)
}

func sendLineCommand(client ble.Client, printChr *ble.Characteristic, cmdId byte, lines uint) error {
	param := []byte{byte(lines & 0xFF), byte(lines >> 8)}
	cmd := buildCommand(cmdId, param)
	return client.WriteCharacteristic(printChr, cmd, true)
}

func renderPreviewFrom1bpp(pixels []byte, width, height int) image.Image {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := (y*width + x) / 8
			bit := uint(x % 8)
			if pixels[idx]&(1<<bit) != 0 {
				img.SetGray(x, y, color.Gray{Y: 0})
			} else {
				img.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	return img
}

func renderPreviewFrom4bpp(pixels []byte, width, height int) image.Image {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := (y*width + x) >> 1
			shift := uint(((x & 1) ^ 1) << 2)
			val := (pixels[idx] >> shift) & 0x0F
			gray := 255 - val*17
			img.SetGray(x, y, color.Gray{Y: gray})
		}
	}
	return img
}

func findPrinter(ctx context.Context) (ble.Advertisement, error) {
	var addr ble.Addr
	var adv ble.Advertisement

	if address != "" {
		log.Printf("Connecting directly to MAC address: %s", address)
		addr = ble.NewAddr(address)
		fmt.Printf("Using address: %s\n", addr)
	}

	ctxScan, cancel := context.WithTimeout(ctx, scanTimeout)
	log.Println("Scanning for printer...")
	err := ble.Scan(ctxScan, false, func(a ble.Advertisement) {
		if address != "" {
			if a.Addr().String() == addr.String() { // Wonder why this works and not direct comparison
				adv = a
				cancel()
			}
		} else if a.LocalName() == targetPrinterName {
			adv = a
			cancel()
		}
	}, nil)
	if err != nil && err != context.Canceled {
		return nil, fmt.Errorf("scan error, %v", err)
	}
	if adv == nil {
		return nil, fmt.Errorf("printer not found")
	}
	log.Println("Found target printer with address:", adv.Addr().String())
	return adv, nil
}

func discoverChars(client ble.Client) (*ble.Characteristic, *ble.Characteristic, *ble.Characteristic, error) {
	var printChr, notifyChr, dataChr *ble.Characteristic
	services, err := client.DiscoverServices([]ble.UUID{mainServiceUUID})
	if err != nil || len(services) == 0 {
		return nil, nil, nil, fmt.Errorf("service discovery failed: %v", err)
	}
	svc := services[0]
	chars, err := client.DiscoverCharacteristics(nil, svc)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("characteristic discovery failed: %v", err)
	}
	for _, c := range chars {
		switch c.UUID.String() {
		case printCharacteristic.String():
			printChr = c
		case notifyCharacteristic.String():
			notifyChr = c
		case dataCharacteristic.String():
			dataChr = c
		}
	}
	return printChr, notifyChr, dataChr, nil
}

func subToNotifs(client ble.Client, notifyChr *ble.Characteristic) error {
	if notifyChr != nil {
		_, _ = client.DiscoverDescriptors(nil, notifyChr)
		err := client.Subscribe(notifyChr, false, func(b []byte) {
			parseNotification(b)
		})
		if err != nil {
			return fmt.Errorf("%v", err)
		} else {
			log.Println("Subscribed to printer notifications.")
		}
	} else {
		return fmt.Errorf("missing notification characteristic")
	}
	return nil
}

func loadAndProcessImage(imagePath string, printMode PrintMode, ditherType string) ([]byte, int, error) {
	img, err := decodeImage(imagePath)

	if err != nil {
		log.Fatalf("Image load error: %v", err)
	}
	img = padImageToMinLines(img, minLines)
	var pixels []byte
	var height int

	// Convert image to the desired format
	switch printMode {
	case Mode1bpp:
		pixels, height, err = loadImageMonoFromImage(img, ditherType)
	case Mode4bpp:
		pixels, height, err = loadImage4BitFromImage(img, ditherType)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("image conversion error: %v", err)
	}

	return pixels, height, nil
}

func loadPrinter() (ble.Client, *ble.Characteristic, *ble.Characteristic, *ble.Characteristic, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Initialize BLE device
	d, err := linux.NewDevice()
	if err != nil {
		log.Fatalf("Failed to open BLE device: %v", err)
	}
	ble.SetDefaultDevice(d)

	// Find printer
	adv, err := findPrinter(ctx)
	if err != nil {
		log.Fatalf("Failed to find printer: %v", err)
	}

	// Connect to printer
	log.Println("Connecting...")
	client, err := ble.Dial(ctx, adv.Addr())
	if err != nil {
		log.Fatalf("Connect failed: %v", err)
	}

	// Negotiate large MTU if possible
	mtu, err := client.ExchangeMTU(100)
	if err != nil {
		log.Printf("MTU negotiation failed: %v", err)
	} else {
		log.Printf("Negotiated ATT MTU: %d", mtu)
	}

	// Discover services and characteristics
	printChr, notifyChr, dataChr, err := discoverChars(client)
	if err != nil {
		log.Fatalf("Characteristic discovery failed: %v", err)
	}

	return client, printChr, notifyChr, dataChr, nil
}

func main() {
	flag.Parse()

	if outputPath != "-" {
		log.Println("Bleh! Cat Printer Utility for MXW01, version", version)
	}

	needNotifications := getStatus || getBattery || getVersion || getPrintType || getQueryCount || ejectPaper > 0 || retractPaper > 0

	needPrinter := needNotifications || (flag.NArg() > 0 && outputPath == "")

	if !needPrinter && outputPath == "" {
		log.Println("Nothing to do. Use -h for help.")
		log.Println("Done!")
		return
	}

	// Get print mode
	var printMode PrintMode
	switch mode {
	case "1bpp":
		printMode = Mode1bpp
	case "4bpp":
		printMode = Mode4bpp
	default:
		fmt.Println("Invalid mode. Use '1bpp' or '4bpp'.")
		return
	}

	// Get image path
	imagePath := flag.Arg(0)

	pixels, height, err := []byte(nil), int(0), error(nil)

	if imagePath != "" {
		pixels, height, err = loadAndProcessImage(imagePath, printMode, ditherType)
		if err != nil {
			log.Fatalf("Failed to load and process image: %v", err)
		}
	}

	if outputPath != "" {
		var previewImg image.Image
		switch printMode {
		case Mode1bpp:
			previewImg = renderPreviewFrom1bpp(pixels, linePixels, height)
		case Mode4bpp:
			previewImg = renderPreviewFrom4bpp(pixels, linePixels, height)
		}
		var out io.Writer
		if outputPath == "-" {
			out = os.Stdout
		} else {
			f, err := os.Create(outputPath)
			if err != nil {
				log.Fatalf("Failed to create output file: %v", err)
			}
			defer f.Close()
			out = f
		}
		err = imaging.Encode(out, previewImg, imaging.PNG)
		if err != nil {
			log.Fatalf("Failed to write PNG preview: %v", err)
		}
		if outputPath != "-" {
			log.Printf("Preview PNG written to %s\n", outputPath)
		}
		return
	}

	if needPrinter {
		client, printChr, notifyChr, dataChr, err := loadPrinter()

		defer client.CancelConnection()

		if err != nil {
			log.Fatalf("Failed to load printer: %v", err)
		}

		if needNotifications {
			// Subscribe to notifications
			err := subToNotifs(client, notifyChr)
			if err != nil {
				log.Fatalf("Failed to subscribe to notifications: %v", err)
			}

			// TODO: check if the firmware allows more than one command at a time
			// Also find a neater way to handle this
			if getStatus {
				sendSimpleCommand(client, printChr, 0xA1)
			}
			if getBattery {
				sendSimpleCommand(client, printChr, 0xAB)
			}
			if getVersion {
				sendSimpleCommand(client, printChr, 0xB1)
			}
			if getPrintType {
				sendSimpleCommand(client, printChr, 0xB0)
			}
			if getQueryCount {
				sendSimpleCommand(client, printChr, 0xA7)
			}
			if ejectPaper > 0 {
				sendLineCommand(client, printChr, 0xA3, ejectPaper)
			}
			if retractPaper > 0 {
				sendLineCommand(client, printChr, 0xA4, retractPaper)
			}
			log.Println("Waiting for notifications...")
			time.Sleep(2 * time.Second)

			if flag.NArg() < 1 {
				return // no image to print
			} else {
				log.Fatalf("Refusing to print and query at the same time due to a firmware bug. Please run print and query commands separately.")
			}
		}
		if printChr == nil {
			log.Fatalf("Missing required print characteristic")
		}

		i := max(intensity, 0)
		i = min(i, 100)
		intensityByte := byte(i)

		if dataChr == nil {
			log.Fatalf("Missing required data characteristic")
		}

		err = sendImageBufferToPrinter(client, dataChr, printChr, pixels, height, printMode, intensityByte)
		if err != nil {
			log.Fatalf("Failed to print image: %v", err)
		}
	}

	log.Println("Done!")
}

func buildCommand(cmdId byte, payload []byte) []byte {
	cmd := append([]byte{}, printCommandHeader...)
	cmd = append(cmd, cmdId)
	cmd = append(cmd, 0x00) // reserved
	cmd = append(cmd, byte(len(payload)&0xFF), byte(len(payload)>>8))
	cmd = append(cmd, payload...)
	cmd = append(cmd, calculateCRC8(payload))
	cmd = append(cmd, printCommandFooter)
	return cmd
}

func calculateCRC8(data []byte) byte {
	table := [256]byte{
		0x00, 0x07, 0x0e, 0x09, 0x1c, 0x1b, 0x12, 0x15,
		0x38, 0x3f, 0x36, 0x31, 0x24, 0x23, 0x2a, 0x2d,
		0x70, 0x77, 0x7e, 0x79, 0x6c, 0x6b, 0x62, 0x65,
		0x48, 0x4f, 0x46, 0x41, 0x54, 0x53, 0x5a, 0x5d,
		0xe0, 0xe7, 0xee, 0xe9, 0xfc, 0xfb, 0xf2, 0xf5,
		0xd8, 0xdf, 0xd6, 0xd1, 0xc4, 0xc3, 0xca, 0xcd,
		0x90, 0x97, 0x9e, 0x99, 0x8c, 0x8b, 0x82, 0x85,
		0xa8, 0xaf, 0xa6, 0xa1, 0xb4, 0xb3, 0xba, 0xbd,
		0xc7, 0xc0, 0xc9, 0xce, 0xdb, 0xdc, 0xd5, 0xd2,
		0xff, 0xf8, 0xf1, 0xf6, 0xe3, 0xe4, 0xed, 0xea,
		0xb7, 0xb0, 0xb9, 0xbe, 0xab, 0xac, 0xa5, 0xa2,
		0x8f, 0x88, 0x81, 0x86, 0x93, 0x94, 0x9d, 0x9a,
		0x27, 0x20, 0x29, 0x2e, 0x3b, 0x3c, 0x35, 0x32,
		0x1f, 0x18, 0x11, 0x16, 0x03, 0x04, 0x0d, 0x0a,
		0x57, 0x50, 0x59, 0x5e, 0x4b, 0x4c, 0x45, 0x42,
		0x6f, 0x68, 0x61, 0x66, 0x73, 0x74, 0x7d, 0x7a,
		0x89, 0x8e, 0x87, 0x80, 0x95, 0x92, 0x9b, 0x9c,
		0xb1, 0xb6, 0xbf, 0xb8, 0xad, 0xaa, 0xa3, 0xa4,
		0xf9, 0xfe, 0xf7, 0xf0, 0xe5, 0xe2, 0xeb, 0xec,
		0xc1, 0xc6, 0xcf, 0xc8, 0xdd, 0xda, 0xd3, 0xd4,
		0x69, 0x6e, 0x67, 0x60, 0x75, 0x72, 0x7b, 0x7c,
		0x51, 0x56, 0x5f, 0x58, 0x4d, 0x4a, 0x43, 0x44,
		0x19, 0x1e, 0x17, 0x10, 0x05, 0x02, 0x0b, 0x0c,
		0x21, 0x26, 0x2f, 0x28, 0x3d, 0x3a, 0x33, 0x34,
		0x4e, 0x49, 0x40, 0x47, 0x52, 0x55, 0x5c, 0x5b,
		0x76, 0x71, 0x78, 0x7f, 0x6a, 0x6d, 0x64, 0x63,
		0x3e, 0x39, 0x30, 0x37, 0x22, 0x25, 0x2c, 0x2b,
		0x06, 0x01, 0x08, 0x0f, 0x1a, 0x1d, 0x14, 0x13,
		0xae, 0xa9, 0xa0, 0xa7, 0xb2, 0xb5, 0xbc, 0xbb,
		0x96, 0x91, 0x98, 0x9f, 0x8a, 0x8d, 0x84, 0x83,
		0xde, 0xd9, 0xd0, 0xd7, 0xc2, 0xc5, 0xcc, 0xcb,
		0xe6, 0xe1, 0xe8, 0xef, 0xfa, 0xfd, 0xf4, 0xf3}
	crc := byte(0)
	for _, b := range data {
		crc = table[crc^b]
	}
	return crc
}
