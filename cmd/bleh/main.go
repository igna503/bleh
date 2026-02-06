package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"io"
	"log"
	"net"
	"os"
	"time"

	"bleh/internal/imageproc"
	"bleh/internal/ipc"
	"bleh/internal/mxw01"

	"github.com/disintegration/imaging"
)

const defaultSock = "/run/bleh/blehd.sock"

var (
	intensity     int
	mode          string
	ditherType    string
	getStatus     bool
	getBattery    bool
	getVersion    bool
	getPrintType  bool
	getQueryCount bool
	ejectPaper    uint
	retractPaper  uint
	outputPath    string
	address       string
	socketPath    string
	jsonOutput    bool
	version       = "dev"
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

	flag.StringVar(&socketPath, "socket", "", "blehd Unix socket path (default: auto-detect)")
	flag.BoolVar(&jsonOutput, "json", false, "Output machine-readable JSON")
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

	// Print mode
	var printMode mxw01.PrintMode
	switch mode {
	case "1bpp":
		printMode = mxw01.Mode1bpp
	case "4bpp":
		printMode = mxw01.Mode4bpp
	default:
		log.Fatalf("Invalid mode. Use '1bpp' or '4bpp'.")
	}

	imagePath := flag.Arg(0)
	var pixels []byte
	var height int
	var err error

	if imagePath != "" {
		img, err := imageproc.DecodeImage(imagePath)
		if err != nil {
			log.Fatalf("Failed to load image: %v", err)
		}
		img = imageproc.PadImageToMinLines(img, mxw01.MinLines)

		switch printMode {
		case mxw01.Mode1bpp:
			pixels, height, err = imageproc.LoadImageMonoFromImage(img, ditherType)
		case mxw01.Mode4bpp:
			pixels, height, err = imageproc.LoadImage4BitFromImage(img, ditherType)
		}
		if err != nil {
			log.Fatalf("Image conversion error: %v", err)
		}
	}

	if outputPath != "" {
		writePreview(printMode, pixels, height)
		return
	}

	c, usedSock, err := dialAuto(socketPath)
	if err != nil {
		log.Fatalf("Could not connect to blehd: %v\nStart it with: ./blehd --socket %s", err, defaultSock)
	}
	_ = usedSock
	defer c.Close()

	id := 1
	call := func(method string, params map[string]any) *ipc.Response {
		req := ipc.Request{ID: fmt.Sprintf("%d", id), Method: method, Params: params}
		id++
		if err := ipc.EncodeLine(c, req); err != nil {
			log.Fatalf("send failed: %v", err)
		}
		resp, err := readResp(c)
		if err != nil {
			log.Fatalf("read failed: %v", err)
		}
		if !resp.OK {
			log.Fatalf("%s failed: %s", method, resp.Error.Message)
		}
		return resp
	}

	params := map[string]any{}
	if address != "" {
		params["address"] = address
	}

	if needNotifications {
		if getStatus {
			resp := call("printer.status.get", params)
			printResult("printer.status.get", resp.Result)
		}
		if getBattery {
			resp := call("printer.battery.get", params)
			printResult("printer.battery.get", resp.Result)
		}
		if getVersion {
			resp := call("printer.version.get", params)
			printResult("printer.version.get", resp.Result)
		}
		if getPrintType {
			resp := call("printer.printtype.get", params)
			printResult("printer.printtype.get", resp.Result)
		}
		if getQueryCount {
			resp := call("printer.querycount.get", params)
			printResult("printer.querycount.get", resp.Result)
		}
		if ejectPaper > 0 {
			p := cloneMap(params)
			p["lines"] = int(ejectPaper)
			resp := call("paper.eject", p)
			printResult("paper.eject", resp.Result)
		}
		if retractPaper > 0 {
			p := cloneMap(params)
			p["lines"] = int(retractPaper)
			resp := call("paper.retract", p)
			printResult("paper.retract", resp.Result)
		}
	}

	if flag.NArg() > 0 {
		i := intensity
		if i < 0 {
			i = 0
		}
		if i > 100 {
			i = 100
		}
		p := cloneMap(params)
		p["mode"] = mode
		p["intensity"] = i
		p["height"] = height
		p["pixels_b64"] = base64.StdEncoding.EncodeToString(pixels)
		resp := call("print.start", p)
		printResult("print.start", resp.Result)
	}

	log.Println("Done!")
}

func writePreview(printMode mxw01.PrintMode, pixels []byte, height int) {
	var previewImg image.Image
	switch printMode {
	case mxw01.Mode1bpp:
		previewImg = imageproc.RenderPreviewFrom1bpp(pixels, mxw01.LinePixels, height)
	case mxw01.Mode4bpp:
		previewImg = imageproc.RenderPreviewFrom4bpp(pixels, mxw01.LinePixels, height)
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
		out = bufio.NewWriter(f)
		defer func() { _ = out.(*bufio.Writer).Flush() }()
	}

	if err := imaging.Encode(out, previewImg, imaging.PNG); err != nil {
		log.Fatalf("Failed to write PNG preview: %v", err)
	}
	if outputPath != "-" {
		log.Printf("Preview PNG written to %s\n", outputPath)
	}
}

func dial(sock string) (net.Conn, error) {
	return net.DialTimeout("unix", sock, 2*time.Second)
}

func dialAuto(sock string) (net.Conn, string, error) {
	if sock != "" {
		c, err := dial(sock)
		return c, sock, err
	}

	candidates := []string{}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		candidates = append(candidates, xdg+"/bleh/blehd.sock")
	}
	candidates = append(candidates, defaultSock)

	var lastErr error
	for _, p := range candidates {
		c, err := dial(p)
		if err == nil {
			return c, p, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no socket candidates")
	}
	return nil, "", lastErr
}

func readResp(c net.Conn) (*ipc.Response, error) {
	r := bufio.NewReader(c)
	line, err := ipc.ReadLine(r)
	if err != nil {
		return nil, err
	}
	var resp ipc.Response
	if err := jsonUnmarshal(line, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

func jsonUnmarshal(b []byte, v any) error {
	// Small helper to avoid importing encoding/json at top-level with name conflicts.
	return json.Unmarshal(b, v)
}

func jsonPretty(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

func printResult(method string, result any) {
	if result == nil {
		return
	}
	if jsonOutput {
		b, _ := jsonPretty(result)
		fmt.Println(string(b))
		return
	}

	m, _ := result.(map[string]any)
	switch method {
	case "printer.status.get":
		fmt.Printf("Status: %v (%s), Battery: %v, Temp: %v\n",
			m["ok"], m["state"], m["battery"], m["temp"])
	case "printer.battery.get":
		fmt.Printf("Battery level: %v\n", m["battery"])
	case "printer.version.get":
		fmt.Printf("Version: %v, Print type: %v\n", m["version"], m["printType"])
	case "printer.printtype.get":
		fmt.Printf("Print type: %v\n", m["printType"])
	case "printer.querycount.get":
		fmt.Printf("Query count: %v\n", m["queryCount"])
	case "paper.eject":
		fmt.Printf("Ejecting paper (%v lines)...\n", m["eject"])
	case "paper.retract":
		fmt.Printf("Retracting paper (%v lines)...\n", m["retract"])
	case "print.start":
		fmt.Println("Printed.")
	default:
		b, _ := jsonPretty(result)
		fmt.Println(string(b))
	}
}
