// airplay-wifi-setup pushes WiFi credentials to an Apple WAC accessory (AirPlay
// speakers etc.) and completes the handshake the vendor apps tend to miss.
//
// Phases:
//  1. join the accessory's setup access point   (hook or manual)
//  2. discover its WAC server, send auth-setup + config over ONE connection
//  3. join the target WiFi                        (hook or manual)
//  4. find the accessory on the LAN, POST /configured so it keeps the credentials
//  5. wait for AirPlay to show up (informational)
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/BertoldVdb/WACResearch/waclib"
)

func main() {
	log.SetFlags(log.Ltime)
	cfg := defaults()

	cfgPath := flag.String("c", "airplay-wifi-setup.conf", "config file (KEY=VALUE)")
	flag.StringVar(&cfg.SSID, "ssid", "", "target WiFi SSID (overrides config)")
	flag.StringVar(&cfg.DeviceName, "name", cfg.DeviceName, "name the accessory should announce")
	flag.StringVar(&cfg.Device, "device", "", "WAC server host:port, skips mDNS discovery")
	flag.BoolVar(&cfg.Insecure, "insecure", false, "skip MFi certificate validation")
	skipAP := flag.Bool("already-on-ap", false, "machine is already connected to the setup AP")
	check := flag.Bool("check", false, "print the resolved configuration (without the password) and exit")
	flag.Parse()

	// flags were parsed into cfg already; keep their values on top of file + env
	flagged := cfg
	if err := cfg.loadFile(*cfgPath); err != nil && !(os.IsNotExist(err) && *cfgPath == "airplay-wifi-setup.conf") {
		log.Fatalf("config: %v", err)
	}
	cfg.loadEnv()
	if flagged.SSID != "" {
		cfg.SSID = flagged.SSID
	}
	if flagged.DeviceName != defaults().DeviceName {
		cfg.DeviceName = flagged.DeviceName
	}
	if flagged.Device != "" {
		cfg.Device = flagged.Device
	}
	cfg.Insecure = cfg.Insecure || flagged.Insecure

	if err := cfg.resolveCredentials(); err != nil {
		log.Fatal(err)
	}
	log.Printf("target network %q (password: %d chars), accessory name %q", cfg.SSID, len(cfg.Password), cfg.DeviceName)
	if *check {
		fmt.Printf("SSID=%q\nPASSWORD=<%d chars>\nDEVICE_NAME=%q\nDEVICE=%q\nINSECURE=%v\nCONFIRM_PORTS=%v\nWAIT_SECONDS=%d\nIFACE=%q\nHOOK_JOIN_AP=%q\nHOOK_JOIN_HOME=%q\nAP_PATTERN=%q\n",
			cfg.SSID, len(cfg.Password), cfg.DeviceName, cfg.Device, cfg.Insecure, cfg.ConfirmPorts, cfg.WaitSeconds, cfg.Iface, cfg.HookJoinAP, cfg.HookJoinHome, cfg.APPattern)
		return
	}

	// ---- 1. setup AP ----------------------------------------------------
	if !*skipAP {
		step(cfg.HookJoinAP, cfg.hookEnv(), "Connect this machine to the accessory's setup WiFi, then press Enter")
	}

	// ---- 2. configure ---------------------------------------------------
	dest, deviceID := cfg.Device, ""
	if dest == "" {
		if s, ok := findWAC("", 8*time.Second); ok {
			dest = net.JoinHostPort(s.Addr.String(), fmt.Sprint(s.Port))
			deviceID = s.TXT["deviceid"]
			log.Printf("found WAC service %q at %s (device id %s, seed %s)", s.Instance, dest, deviceID, s.TXT["seed"])
		} else if gw, _, err := defaultGateway(); err == nil {
			dest = net.JoinHostPort(gw.String(), "5000")
			log.Printf("no mDNS answer, assuming WAC server at gateway %s", dest)
		} else {
			log.Fatal("cannot find the accessory: no mDNS record and no default gateway. Use -device host:port")
		}
	}

	client := waclib.NewClient()
	client.Insecure = cfg.Insecure
	resp, err := client.Configure(dest, waclib.ConfigRequest{
		DeviceName: cfg.DeviceName, SSID: cfg.SSID, Password: cfg.Password,
	})
	client.Close()
	if err != nil {
		log.Printf("configure failed: %v", err)
		log.Fatal("hint: the setup-mode server survives exactly one connection. Power-cycle the accessory and run again without probing it first.")
	}
	log.Print("credentials delivered")
	if resp != nil {
		for _, kv := range [][2]string{{"model", resp.Model}, {"firmware", resp.FirmwareVersion},
			{"hardware", resp.HardwareVersion}, {"serial", resp.SerialNumber}, {"name", resp.DeviceName}} {
			if kv[1] != "" {
				log.Printf("  %-9s %s", kv[0], kv[1])
			}
		}
	}

	// ---- 3. back to the target network -----------------------------------
	step(cfg.HookJoinHome, cfg.hookEnv(), fmt.Sprintf("Connect this machine to %q now, then press Enter", cfg.SSID))

	// ---- 4. confirm -----------------------------------------------------
	log.Printf("looking for the accessory on %q (up to %ds)...", cfg.SSID, cfg.WaitSeconds)
	deadline := time.Now().Add(time.Duration(cfg.WaitSeconds) * time.Second)
	var ip net.IP
	for time.Now().Before(deadline) {
		ports := cfg.ConfirmPorts
		if s, ok := findWAC(deviceID, 3*time.Second); ok {
			ip = s.Addr
			ports = append([]int{s.Port}, ports...)
			log.Printf("mDNS: accessory at %s, WAC port %d, seed %s", ip, s.Port, s.TXT["seed"])
		} else if ip == nil && deviceID != "" {
			if a, ok := findByMAC(cfg.Iface, deviceID); ok {
				ip = a
				log.Printf("ARP: accessory at %s", ip)
			}
		}
		if ip == nil {
			continue
		}
		for _, p := range ports {
			if err := postConfigured(ip, p); err == nil {
				log.Printf("/configured accepted on %s:%d. The accessory now keeps the credentials.", ip, p)
				verify(deviceID, ip)
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	log.Fatal("gave up: the accessory never accepted /configured. It will probably forget the WiFi on the next power-cycle. Check router client isolation and retry.")
}

// step runs a hook command, or asks the user to do it by hand.
func step(hook string, env []string, prompt string) {
	if hook != "" {
		log.Printf("running hook: %s", hook)
		cmd := exec.Command("sh", "-c", hook)
		cmd.Env, cmd.Stdout, cmd.Stderr = env, os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("hook failed: %v", err)
		}
		time.Sleep(2 * time.Second)
		return
	}
	fmt.Fprintf(os.Stderr, "\n>>> %s ", prompt)
	bufio.NewReader(os.Stdin).ReadString('\n')
}

// verify waits a bit for AirPlay/RAOP to appear; purely informational.
func verify(deviceID string, ip net.IP) {
	log.Print("waiting for the accessory's services to start...")
	end := time.Now().Add(60 * time.Second)
	for time.Now().Before(end) {
		for _, svc := range []string{"_airplay._tcp", "_raop._tcp", "_googlecast._tcp", "_spotify-connect._tcp"} {
			entries, _ := browse(svc, 3*time.Second)
			for _, e := range entries {
				if e.Addr.Equal(ip) || strings.EqualFold(e.TXT["deviceid"], deviceID) {
					model := e.TXT["model"] + e.TXT["am"]
					if model != "" {
						model = " (model " + model + ")"
					}
					log.Printf("%s announced by %q at %s:%d%s", svc, e.Instance, e.Addr, e.Port, model)
					return
				}
			}
		}
	}
	log.Print("no AirPlay announcement seen yet; give it a minute and check your player")
}
