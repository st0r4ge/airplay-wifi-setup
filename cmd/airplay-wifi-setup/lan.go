package main

// Finding the accessory once it has joined the target network, and sending
// the final /configured request. mDNS first, ARP by MAC as fallback.

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

type service struct {
	Instance string
	Addr     net.IP
	Port     int
	TXT      map[string]string
}

// browse collects mDNS entries of one service type for the given duration.
func browse(svc string, d time.Duration) ([]service, error) {
	r, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	ch := make(chan *zeroconf.ServiceEntry)
	if err := r.Browse(ctx, svc, "local", ch); err != nil {
		return nil, err
	}
	var out []service
	for e := range ch {
		s := service{Instance: strings.ReplaceAll(e.Instance, "\\", ""), Port: e.Port, TXT: map[string]string{}}
		for _, t := range e.Text {
			if k, v, ok := strings.Cut(t, "="); ok {
				s.TXT[strings.ToLower(k)] = v
			}
		}
		if len(e.AddrIPv4) > 0 {
			s.Addr = e.AddrIPv4[0]
		} else if len(e.AddrIPv6) > 0 {
			s.Addr = e.AddrIPv6[0]
		}
		out = append(out, s)
	}
	return out, nil
}

// findWAC returns the first _mfi-config._tcp entry (matching deviceID if given).
func findWAC(deviceID string, d time.Duration) (*service, bool) {
	entries, _ := browse("_mfi-config._tcp", d)
	for i := range entries {
		e := &entries[i]
		if deviceID == "" || strings.EqualFold(e.TXT["deviceid"], deviceID) {
			if e.Addr != nil {
				return e, true
			}
		}
	}
	return nil, false
}

// defaultGateway returns the IPv4 default gateway from /proc/net/route (Linux).
func defaultGateway() (net.IP, string, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		b, err := hex.DecodeString(fields[2])
		if err != nil || len(b) != 4 {
			continue
		}
		return net.IPv4(b[3], b[2], b[1], b[0]), fields[0], nil
	}
	return nil, "", fmt.Errorf("no default route")
}

// ifaceNet returns the IPv4 network of iface (or of the default-route interface).
func ifaceNet(iface string) (*net.IPNet, error) {
	if iface == "" {
		_, name, err := defaultGateway()
		if err != nil {
			return nil, err
		}
		iface = name
	}
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, err
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			return n, nil
		}
	}
	return nil, fmt.Errorf("%s has no IPv4 address", iface)
}

// findByMAC pokes every host of the local /24 (or smaller) with a UDP packet so
// the kernel resolves ARP, then looks the MAC up in /proc/net/arp. Unprivileged.
func findByMAC(iface, mac string) (net.IP, bool) {
	n, err := ifaceNet(iface)
	if err != nil {
		return nil, false
	}
	ones, _ := n.Mask.Size()
	if ones < 24 {
		n.Mask = net.CIDRMask(24, 32) // don't sweep more than 254 hosts
	}
	base := n.IP.Mask(n.Mask).To4()
	hosts := 1 << (32 - 24)
	if ones > 24 {
		hosts = 1 << (32 - ones)
	}
	start := binary.BigEndian.Uint32(base)
	for i := uint32(1); i < uint32(hosts)-1; i++ {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, start+i)
		if c, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: ip, Port: 9}); err == nil {
			_, _ = c.Write([]byte{0})
			c.Close()
		}
	}
	time.Sleep(1200 * time.Millisecond)

	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return nil, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 4 && strings.EqualFold(fields[3], mac) && fields[2] != "0x0" {
			return net.ParseIP(fields[0]), true
		}
	}
	return nil, false
}

// postConfigured sends the final WAC message. Only a 200 counts.
func postConfigured(ip net.IP, port int) error {
	cl := http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://%s/configured", net.JoinHostPort(ip.String(), fmt.Sprint(port)))
	resp, err := cl.Post(url, "application/octet-stream", strings.NewReader(""))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return nil
}
