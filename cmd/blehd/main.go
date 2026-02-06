package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"bleh/internal/ipc"
	"bleh/internal/mxw01"

	ble "github.com/go-ble/ble"
	"github.com/go-ble/ble/linux"
	"os/user"
)

const (
	defaultSock = "/run/bleh/blehd.sock"
)

func autoSocketPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		return filepath.Join(xdg, "bleh", "blehd.sock"), nil
	}
	return defaultSock, nil
}

var printerMu sync.Mutex

func main() {
	var socketPath string
	var groupName string
	flag.StringVar(&socketPath, "socket", "", "Unix socket path (default: auto-detect)")
	flag.StringVar(&groupName, "group", "bleh", "Group name to own the socket")
	flag.Parse()

	resolvedSock, err := autoSocketPath(socketPath)
	if err != nil {
		log.Fatalf("Failed to resolve socket path: %v", err)
	}
	socketPath = resolvedSock

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Init BLE device once.
	d, err := linux.NewDevice()
	if err != nil {
		log.Printf("Failed to open BLE device: %v", err)
		log.Printf("Hint: blehd needs CAP_NET_RAW and CAP_NET_ADMIN. Example:")
		log.Printf("  sudo setcap cap_net_raw,cap_net_admin=eip $(which blehd)")
		log.Fatalf("Exiting.")
	}
	ble.SetDefaultDevice(d)

	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		log.Fatalf("Failed to create socket dir: %v", err)
	}
	// Remove stale socket.
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("Listen failed: %v", err)
	}
	defer ln.Close()

	// Set group ownership + perms.
	if err := setSocketPerms(socketPath, groupName); err != nil {
		log.Printf("Warning: failed to set socket permissions: %v", err)
		log.Printf("Hint: if running unprivileged, prefer an XDG runtime socket (set $XDG_RUNTIME_DIR)")
	}

	log.Printf("blehd listening on %s", socketPath)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = os.Remove(socketPath)
	}()

	for {
		c, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			log.Printf("accept error: %v", err)
			continue
		}
		go handleConn(ctx, c)
	}
}

func setSocketPerms(path, group string) error {
	grp, err := user.LookupGroup(group)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(grp.Gid)
	if err != nil {
		return err
	}
	// Keep uid unchanged, set gid.
	if err := os.Chown(path, -1, gid); err != nil {
		return err
	}
	return os.Chmod(path, 0o660)
}

func handleConn(ctx context.Context, c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	w := bufio.NewWriter(c)
	defer w.Flush()

	for {
		line, err := ipc.ReadLine(r)
		if err != nil {
			return
		}
		req, err := ipc.DecodeRequestLine(line)
		if err != nil {
			_ = ipc.EncodeLine(w, ipc.Response{OK: false, Error: &ipc.Error{Code: "EBADJSON", Message: err.Error()}})
			_ = w.Flush()
			continue
		}

		resp := dispatch(ctx, req)
		_ = ipc.EncodeLine(w, resp)
		_ = w.Flush()
	}
}

func dispatch(ctx context.Context, req *ipc.Request) ipc.Response {
	if req.ID == "" {
		req.ID = "-"
	}
	method := req.Method

	// Serialize access to the printer to avoid concurrent BLE sessions stomping each other.
	// (This can be relaxed later if the printer/stack supports it reliably.)
	printerMu.Lock()
	defer printerMu.Unlock()

	switch method {
	case "printer.status.get":
		st, err := doQuery(ctx, req.Params, 0xA1)
		if err != nil {
			return fail(req.ID, err)
		}
		s, err := mxw01.DecodeStatusNotification(st)
		if err != nil {
			return fail(req.ID, err)
		}
		return ok(req.ID, s)

	case "printer.battery.get":
		b, err := doQuery(ctx, req.Params, 0xAB)
		if err != nil {
			return fail(req.ID, err)
		}
		pct, err := mxw01.DecodeBatteryNotification(b)
		if err != nil {
			return fail(req.ID, err)
		}
		return ok(req.ID, map[string]any{"battery": pct})

	case "printer.version.get":
		b, err := doQuery(ctx, req.Params, 0xB1)
		if err != nil {
			return fail(req.ID, err)
		}
		v, err := mxw01.DecodeVersionNotification(b)
		if err != nil {
			return fail(req.ID, err)
		}
		return ok(req.ID, v)

	case "printer.printtype.get":
		b, err := doQuery(ctx, req.Params, 0xB0)
		if err != nil {
			return fail(req.ID, err)
		}
		t, err := mxw01.DecodePrintTypeNotification(b)
		if err != nil {
			return fail(req.ID, err)
		}
		return ok(req.ID, map[string]any{"printType": t})

	case "printer.querycount.get":
		b, err := doQuery(ctx, req.Params, 0xA7)
		if err != nil {
			return fail(req.ID, err)
		}
		qc, err := mxw01.DecodeQueryCountNotification(b)
		if err != nil {
			return fail(req.ID, err)
		}
		return ok(req.ID, map[string]any{"queryCount": fmt.Sprintf("% X", qc)})

	case "paper.eject":
		lines, _ := getInt(req.Params, "lines")
		err := doLineCommand(ctx, req.Params, 0xA3, uint(lines))
		if err != nil {
			return fail(req.ID, err)
		}
		return ok(req.ID, map[string]any{"eject": lines})

	case "paper.retract":
		lines, _ := getInt(req.Params, "lines")
		err := doLineCommand(ctx, req.Params, 0xA4, uint(lines))
		if err != nil {
			return fail(req.ID, err)
		}
		return ok(req.ID, map[string]any{"retract": lines})

	case "print.start":
		err := doPrint(ctx, req.Params)
		if err != nil {
			return fail(req.ID, err)
		}
		return ok(req.ID, map[string]any{"printed": true})

	default:
		return ipc.Response{ID: req.ID, OK: false, Error: &ipc.Error{Code: "ENOIMPL", Message: "unknown method"}}
	}
}

func ok(id string, result any) ipc.Response {
	return ipc.Response{ID: id, OK: true, Result: result}
}

func fail(id string, err error) ipc.Response {
	return ipc.Response{ID: id, OK: false, Error: &ipc.Error{Code: "EFAIL", Message: err.Error()}}
}

func getString(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func getInt(m map[string]any, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	case json.Number:
		i, err := t.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func doQuery(ctx context.Context, params map[string]any, cmd byte) ([]byte, error) {
	addr, _ := getString(params, "address")

	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	adv, err := mxw01.FindPrinter(cctx, addr)
	if err != nil {
		return nil, err
	}

	client, err := ble.Dial(cctx, adv.Addr())
	if err != nil {
		return nil, err
	}
	defer client.CancelConnection()

	mtu, err := client.ExchangeMTU(100)
	if err == nil {
		_ = mtu
	}

	printChr, notifyChr, _, err := mxw01.DiscoverChars(client)
	if err != nil {
		return nil, err
	}
	if notifyChr == nil {
		return nil, fmt.Errorf("missing notification characteristic")
	}

	notifs := make(chan []byte, 8)
	_, _ = client.DiscoverDescriptors(nil, notifyChr)
	err = client.Subscribe(notifyChr, false, func(b []byte) {
		select {
		case notifs <- append([]byte{}, b...):
		default:
		}
	})
	if err != nil {
		return nil, err
	}

	if err := mxw01.SendSimpleCommand(client, printChr, cmd); err != nil {
		return nil, err
	}

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case b := <-notifs:
			c, _, perr := mxw01.ParseNotification(b)
			if perr == nil && c == cmd {
				return b, nil
			}
		case <-deadline.C:
			return nil, fmt.Errorf("timeout waiting for notification 0x%02X", cmd)
		case <-cctx.Done():
			return nil, cctx.Err()
		}
	}
}

func doLineCommand(ctx context.Context, params map[string]any, cmd byte, lines uint) error {
	addr, _ := getString(params, "address")

	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	adv, err := mxw01.FindPrinter(cctx, addr)
	if err != nil {
		return err
	}
	client, err := ble.Dial(cctx, adv.Addr())
	if err != nil {
		return err
	}
	defer client.CancelConnection()

	printChr, _, _, err := mxw01.DiscoverChars(client)
	if err != nil {
		return err
	}
	return mxw01.SendLineCommand(client, printChr, cmd, lines)
}

func doPrint(ctx context.Context, params map[string]any) error {
	addr, _ := getString(params, "address")
	modeStr, _ := getString(params, "mode")
	intensity, _ := getInt(params, "intensity")
	height, _ := getInt(params, "height")
	pixelsB64, _ := getString(params, "pixels_b64")

	if height <= 0 {
		return fmt.Errorf("invalid height")
	}
	pixels, err := base64.StdEncoding.DecodeString(pixelsB64)
	if err != nil {
		return fmt.Errorf("decode pixels: %v", err)
	}

	pm := mxw01.Mode1bpp
	switch modeStr {
	case "1bpp":
		pm = mxw01.Mode1bpp
	case "4bpp":
		pm = mxw01.Mode4bpp
	default:
		return fmt.Errorf("invalid mode")
	}
	if intensity < 0 {
		intensity = 0
	}
	if intensity > 100 {
		intensity = 100
	}

	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	adv, err := mxw01.FindPrinter(cctx, addr)
	if err != nil {
		return err
	}
	client, err := ble.Dial(cctx, adv.Addr())
	if err != nil {
		return err
	}
	defer client.CancelConnection()

	mtu, err := client.ExchangeMTU(100)
	if err != nil {
		mtu = 23
	}

	printChr, _, dataChr, err := mxw01.DiscoverChars(client)
	if err != nil {
		return err
	}
	if printChr == nil || dataChr == nil {
		return fmt.Errorf("missing required characteristics")
	}

	return mxw01.SendImageBufferToPrinter(client, dataChr, printChr, pixels, height, pm, byte(intensity), mtu)
}
