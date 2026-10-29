package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Config is everything the tool needs. Values come from (lowest to highest
// priority): built-in defaults, the config file, WAC_* environment variables,
// command-line flags.
type Config struct {
	SSID         string // target WiFi
	Password     string // target WiFi password (WPA2 PSK)
	PasswordCmd  string // alternative: shell command that prints the password
	NMConnection string // alternative: NetworkManager connection to read SSID+PSK from

	DeviceName string // name the accessory should announce
	Device     string // host:port of the WAC server; empty = discover via mDNS
	Insecure   bool   // skip MFi certificate validation

	ConfirmPorts []int  // ports to try for the final /configured on the LAN
	WaitSeconds  int    // how long to wait for the accessory on the LAN
	Iface        string // network interface used for the ARP fallback (empty = default route)

	HookJoinAP   string // shell command that connects the machine to the setup AP
	HookJoinHome string // shell command that connects the machine back to the target WiFi
	APPattern    string // handed to the hook as WAC_AP_PATTERN
}

func defaults() Config {
	return Config{
		DeviceName:   "Speaker",
		ConfirmPorts: []int{5001, 5000, 5002, 5003, 5004, 5005},
		WaitSeconds:  300,
	}
}

// loadFile reads KEY=VALUE lines. '#' starts a comment, quotes around values are stripped.
func (c *Config) loadFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		s := strings.TrimSpace(sc.Text())
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected KEY=VALUE", path, line)
		}
		if err := c.set(strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)); err != nil {
			return fmt.Errorf("%s:%d: %v", path, line, err)
		}
	}
	return sc.Err()
}

// loadEnv applies WAC_<KEY> environment variables.
func (c *Config) loadEnv() {
	for _, k := range keys {
		if v, ok := os.LookupEnv("WAC_" + k); ok {
			_ = c.set(k, v)
		}
	}
}

var keys = []string{"SSID", "PASSWORD", "PASSWORD_CMD", "NM_CONNECTION", "DEVICE_NAME", "DEVICE",
	"INSECURE", "CONFIRM_PORTS", "WAIT_SECONDS", "IFACE", "HOOK_JOIN_AP", "HOOK_JOIN_HOME", "AP_PATTERN"}

func (c *Config) set(key, val string) error {
	switch strings.ToUpper(key) {
	case "SSID":
		c.SSID = val
	case "PASSWORD":
		c.Password = val
	case "PASSWORD_CMD":
		c.PasswordCmd = val
	case "NM_CONNECTION":
		c.NMConnection = val
	case "DEVICE_NAME":
		c.DeviceName = val
	case "DEVICE":
		c.Device = val
	case "INSECURE":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("INSECURE: %v", err)
		}
		c.Insecure = b
	case "CONFIRM_PORTS":
		c.ConfirmPorts = c.ConfirmPorts[:0]
		for _, p := range strings.Split(val, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				return fmt.Errorf("CONFIRM_PORTS: %v", err)
			}
			c.ConfirmPorts = append(c.ConfirmPorts, n)
		}
	case "WAIT_SECONDS":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("WAIT_SECONDS: %v", err)
		}
		c.WaitSeconds = n
	case "IFACE":
		c.Iface = val
	case "HOOK_JOIN_AP":
		c.HookJoinAP = val
	case "HOOK_JOIN_HOME":
		c.HookJoinHome = val
	case "AP_PATTERN":
		c.APPattern = val
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

// resolveCredentials fills SSID/Password from NM_CONNECTION or PASSWORD_CMD if needed.
func (c *Config) resolveCredentials() error {
	if c.NMConnection != "" {
		if c.SSID == "" {
			out, err := exec.Command("nmcli", "-g", "802-11-wireless.ssid", "con", "show", c.NMConnection).Output()
			if err != nil {
				return fmt.Errorf("nmcli: cannot read SSID of %q: %v", c.NMConnection, err)
			}
			c.SSID = strings.TrimSpace(string(out))
		}
		if c.Password == "" && c.PasswordCmd == "" {
			out, err := exec.Command("nmcli", "-s", "-g", "802-11-wireless-security.psk", "con", "show", c.NMConnection).Output()
			if err != nil {
				return fmt.Errorf("nmcli: cannot read PSK of %q: %v", c.NMConnection, err)
			}
			c.Password = strings.TrimSpace(string(out))
		}
	}
	if c.Password == "" && c.PasswordCmd != "" {
		out, err := exec.Command("sh", "-c", c.PasswordCmd).Output()
		if err != nil {
			return fmt.Errorf("PASSWORD_CMD failed: %v", err)
		}
		c.Password = strings.TrimSpace(string(out))
	}
	if c.SSID == "" {
		return fmt.Errorf("no target SSID: set SSID (or NM_CONNECTION) in the config file")
	}
	return nil
}

// hookEnv is what hook scripts get to see.
func (c *Config) hookEnv() []string {
	return append(os.Environ(),
		"WAC_SSID="+c.SSID,
		"WAC_AP_PATTERN="+c.APPattern,
		"WAC_IFACE="+c.Iface,
		"WAC_NM_CONNECTION="+c.NMConnection,
	)
}
