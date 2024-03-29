package netconf

import (
	"bytes"
	"errors"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/sveil/os/pkg/log"

	shlex "github.com/flynn/go-shlex"
	glob "github.com/ryanuber/go-glob"
	"github.com/vishvananda/netlink"
)

const (
	CONF = "/var/lib/rancher/conf"
	MODE = "mode"
)

var (
	defaultDhcpArgs = []string{"dhcpcd", "-MA4"}
	exitDhcpArgs    = []string{"dhcpcd", "-x"}
	exitWpaArgs     = []string{"wpa_cli", "terminate"}
	dhcpReleaseCmd  = "dhcpcd --release"
)

func createInterfaces(netCfg *NetworkConfig) {
	configured := map[string]bool{}

	for name, iface := range netCfg.Interfaces {
		if iface.Bridge == "true" {
			if _, err := NewBridge(name); err != nil {
				log.Errorf("Failed to create bridge %s: %v", name, err)
			}
		} else if iface.Bridge != "" {
			if _, err := NewBridge(iface.Bridge); err != nil {
				log.Errorf("Failed to create bridge %s: %v", iface.Bridge, err)
			}
		} else if iface.Bond != "" {
			bond, err := Bond(iface.Bond)
			if err != nil {
				log.Errorf("Failed to create bond %s: %v", iface.Bond, err)
				continue
			}

			if !configured[iface.Bond] {
				if bondIface, ok := netCfg.Interfaces[iface.Bond]; ok {
					// Other settings depends on mode, so set it first
					if v, ok := bondIface.BondOpts[MODE]; ok {
						bond.Opt(MODE, v)
					}

					for k, v := range bondIface.BondOpts {
						if k != MODE {
							bond.Opt(k, v)
						}
					}
					configured[iface.Bond] = true
				}
			}
		}
	}
}

func createSlaveInterfaces(netCfg *NetworkConfig) {
	links, err := GetValidLinkList()
	if err != nil {
		log.Errorf("Failed to list links: %v", err)
		return
	}

	for _, link := range links {
		match, ok := findMatch(link, netCfg)
		if !ok {
			continue
		}

		vlanDefs, err := ParseVlanDefinitions(match.Vlans)
		if err != nil {
			log.Errorf("Failed to create vlans on device %s: %v", link.Attrs().Name, err)
			continue
		}

		for _, vlanDef := range vlanDefs {
			if _, err = NewVlan(link, vlanDef.Name, vlanDef.ID); err != nil {
				log.Errorf("Failed to create vlans on device %s, id %d: %v", link.Attrs().Name, vlanDef.ID, err)
			}
		}
	}
}

func findMatch(link netlink.Link, netCfg *NetworkConfig) (InterfaceConfig, bool) {
	linkName := link.Attrs().Name
	var match InterfaceConfig
	exactMatch := false
	found := false

	for key, netConf := range netCfg.Interfaces {
		if netConf.Match == "" {
			netConf.Match = key
		}

		if netConf.Match == "" {
			continue
		}

		if strings.HasPrefix(netConf.Match, "mac") {
			if strings.Contains(netConf.Match, "*") {
				// If selector contains wildcard * and MAC address matches wildcard then return
				// Don't match mac address of a bond or VLAN interface because it is the same address as the slave or parent.
				if glob.Glob(netConf.Match[4:], link.Attrs().HardwareAddr.String()) && link.Attrs().Name != netConf.Bond && link.Type() != "vlan" {
					return netConf, true
				}
				continue
			}

			haAddr, err := net.ParseMAC(netConf.Match[4:])
			if err != nil {
				log.Errorf("Failed to parse mac %s: %v", netConf.Match[4:], err)
				continue
			}

			// Don't match mac address of a bond or VLAN interface because it is the same address as the slave or parent.
			if bytes.Compare(haAddr, link.Attrs().HardwareAddr) == 0 && link.Attrs().Name != netConf.Bond && link.Type() != "vlan" {
				// MAC address match is used over all other matches
				return netConf, true
			}
		}

		if !exactMatch && glob.Glob(netConf.Match, linkName) {
			match = netConf
			found = true
		}

		if netConf.Match == linkName {
			// Found exact match, use it over wildcard match
			match = netConf
			exactMatch = true
		}
	}

	return match, exactMatch || found
}

func populateDefault(netCfg *NetworkConfig) {
	if netCfg.Interfaces == nil {
		netCfg.Interfaces = map[string]InterfaceConfig{}
	}

	if len(netCfg.Interfaces) == 0 {
		netCfg.Interfaces["eth*"] = InterfaceConfig{
			DHCP: true,
		}
	}

	if _, ok := netCfg.Interfaces["lo"]; !ok {
		netCfg.Interfaces["lo"] = InterfaceConfig{
			Addresses: []string{
				"127.0.0.1/8",
				"::1/128",
			},
		}
	}
}

func ApplyNetworkConfigs(netCfg *NetworkConfig, userSetHostname, userSetDNS bool) (bool, error) {
	populateDefault(netCfg)

	log.Debugf("Config: %#v", netCfg)
	runCmds(netCfg.PreCmds, "")
	defer runCmds(netCfg.PostCmds, "")

	createInterfaces(netCfg)
	createSlaveInterfaces(netCfg)

	links, err := GetValidLinkList()
	if err != nil {
		log.Errorf("error getting LinkList: %s", err)
		return false, err
	}

	wg := sync.WaitGroup{}

	//apply network config
	for _, link := range links {
		if !strings.Contains(link.Attrs().Name, "wlan") {
			applyOuter(link, netCfg, &wg, userSetHostname, userSetDNS)
		}
	}
	wg.Wait()

	// apply wifi network config
	for _, link := range links {
		if strings.Contains(link.Attrs().Name, "wlan") {
			applyOuter(link, netCfg, &wg, userSetHostname, userSetDNS)
		}
	}
	wg.Wait()

	// make sure there was a DHCP set dns - or tell ros to write 8.8.8.8,8.8.8.4
	log.Infof("Checking to see if DNS was set by DHCP")
	dnsSet := false
	for _, link := range links {
		linkName := link.Attrs().Name
		log.Infof("dns testing %s", linkName)
		lease := GetDhcpLease(linkName)
		if _, ok := lease["domain_name_servers"]; ok {
			log.Infof("dns was dhcp set for %s", linkName)
			dnsSet = true
		}
	}

	return dnsSet, nil
}

func applyOuter(link netlink.Link, netCfg *NetworkConfig, wg *sync.WaitGroup, userSetHostname, userSetDNS bool) {
	linkName := link.Attrs().Name
	log.Debugf("applyOuter(%v, %v), link: %s", userSetHostname, userSetDNS, linkName)
	match, ok := findMatch(link, netCfg)
	if !ok {
		return
	}

	log.Debugf("Config(%s): %#v", linkName, match)

	// We plan to use the dhcpcd hook to control the wpa_supplicant, Whether the Wi-Fi network uses DHCP or Static
	// https://wiki.archlinux.org/index.php/Dhcpcd#Hooks.
	if match.WifiNetwork != "" {
		match.DHCP = true
	}

	runCmds(match.PreUp, linkName)
	defer runCmds(match.PostUp, linkName)

	if !match.DHCP {
		if err := applyInterfaceConfig(link, match); err != nil {
			log.Errorf("Failed to apply settings to %s : %v", linkName, err)
		}
	}

	if !match.DHCP && !hasDhcp(linkName) {
		log.Debugf("Skipping(%s): DHCP=false && no DHCP lease yet", linkName)
		return
	}

	wg.Add(1)
	go func(link netlink.Link, match InterfaceConfig) {
		if match.DHCP {
			if match.WifiNetwork != "" {
				runWifiDhcp(netCfg, link, match.WifiNetwork, !userSetHostname, !userSetDNS)
			} else {
				runDhcp(netCfg, link.Attrs().Name, match.DHCPArgs, !userSetHostname, !userSetDNS)
			}
		} else {
			log.Infof("dhcp release %s", link.Attrs().Name)
			runDhcp(netCfg, link.Attrs().Name, dhcpReleaseCmd, false, true)
		}
		wg.Done()
	}(link, match)
}

func GetDhcpLease(iface string) (lease map[string]string) {
	lease = make(map[string]string)

	out := getDhcpLeaseString(iface)
	log.Debugf("getDhcpLease %s: %s", iface, out)

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		l := strings.SplitN(line, "=", 2)
		log.Debugf("line: %v", l)
		if len(l) > 1 {
			lease[l[0]] = l[1]
		}
	}
	return lease
}

func getDhcpLeaseString(iface string) []byte {
	args := defaultDhcpArgs
	args = append(args, "-U", iface)
	cmd := exec.Command(args[0], args[1:]...)
	//cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	log.Debugf("Running cmd: %s, output: %s", args, string(out))
	if err != nil {
		// dhcpcd works fine, but gets an error: exit status 1
		log.Warnf("Failed to run cmd: %s, error: %v", args, err)
	}
	return out
}

func hasDhcp(iface string) bool {
	out := getDhcpLeaseString(iface)
	return len(out) > 0
}

func runDhcp(netCfg *NetworkConfig, iface string, argstr string, setHostname, setDNS bool) {
	args := []string{}
	if argstr != "" {
		var err error
		args, err = shlex.Split(argstr)
		if err != nil {
			log.Errorf("Failed to parse [%s]: %v", argstr, err)
		}
	}
	if len(args) == 0 {
		args = defaultDhcpArgs
	}

	if setHostname {
		args = append(args, "-e", "force_hostname=true")
	}

	if !setDNS {
		args = append(args, "--nohook", "resolv.conf")
	}

	if netCfg.DHCPTimeout > 0 {
		args = append(args, "--timeout", strconv.Itoa(netCfg.DHCPTimeout))
	}

	// Wait for lease
	// TODO: this should be optional - based on kernel arg?
	args = append(args, "-w", "--debug")

	args = append(args, iface)
	cmd := exec.Command(args[0], args[1:]...)
	log.Infof("Running DHCP on %s: %s", iface, strings.Join(args, " "))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Errorf("Failed to run dhcpcd for %s: %v", iface, err)
	}
}

func runWifiDhcp(netCfg *NetworkConfig, link netlink.Link, network string, setHostname, setDNS bool) {
	iface := link.Attrs().Name
	if _, ok := netCfg.WifiNetworks[network]; !ok {
		return
	}

	// Remove DHCP lease IP and static IP
	if hasDhcp(iface) {
		runDhcp(netCfg, iface, dhcpReleaseCmd, false, true)
	}
	existAddress, _ := getLinkAddrs(link)
	for _, addr := range existAddress {
		log.Infof("removing  %s from %s", addr.String(), link.Attrs().Name)
		removeAddress(addr, link)
	}

	runDhcp(netCfg, iface, "", setHostname, setDNS)
}

func linkUp(link netlink.Link, netConf InterfaceConfig) error {
	if err := netlink.LinkSetUp(link); err != nil {
		log.Errorf("failed to setup link: %v", err)
		return err
	}

	return nil
}

func applyAddress(address string, link netlink.Link, netConf InterfaceConfig) error {
	addr, err := netlink.ParseAddr(address)
	if err != nil {
		return err
	}
	if err := netlink.AddrAdd(link, addr); err == syscall.EEXIST {
		//Ignore this error
	} else if err != nil {
		log.Errorf("addr add failed: %v", err)
	} else {
		log.Infof("Set %s on %s", netConf.Address, link.Attrs().Name)
	}

	return nil
}

func removeAddress(addr netlink.Addr, link netlink.Link) error {
	if err := netlink.AddrDel(link, &addr); err == syscall.EEXIST {
		//Ignore this error
	} else if err != nil {
		log.Errorf("addr del failed: %v", err)
	} else {
		log.Infof("Removed %s from %s", addr.String(), link.Attrs().Name)
	}

	return nil
}

// setGateway(add=false) will set _one_ gateway on an interface (ie, replace an existing one)
// setGateway(add=true) will add another gateway to an interface
func setGateway(gateway string, add bool) error {
	if gateway == "" {
		return nil
	}

	gatewayIP := net.ParseIP(gateway)
	if gatewayIP == nil {
		return errors.New("Invalid gateway address " + gateway)
	}

	route := netlink.Route{
		Scope: netlink.SCOPE_UNIVERSE,
		Gw:    gatewayIP,
	}

	if add {
		if err := netlink.RouteAdd(&route); err == syscall.EEXIST {
			//Ignore this error
		} else if err != nil {
			log.Errorf("gateway add failed: %v", err)
			return err
		}
		log.Infof("Added default gateway %s", gateway)
	} else {
		if err := netlink.RouteReplace(&route); err == syscall.EEXIST {
			//Ignore this error
		} else if err != nil {
			log.Errorf("gateway replace failed: %v", err)
			return err
		}
		log.Infof("Replaced default gateway %s", gateway)
	}

	return nil
}

func applyInterfaceConfig(link netlink.Link, netConf InterfaceConfig) error {
	//TODO: skip doing anything if the settings are "default"?
	//TODO: how do you undo a non-default with a default?
	// ATM, this removes

	// TODO: undo
	if netConf.Bond != "" {
		if err := netlink.LinkSetDown(link); err != nil {
			return err
		}

		b, err := Bond(netConf.Bond)
		if err != nil {
			return err
		}
		return b.AddSlave(link.Attrs().Name)
	}

	//TODO: undo
	if netConf.Bridge != "" && netConf.Bridge != "true" {
		b, err := NewBridge(netConf.Bridge)
		if err != nil {
			return err
		}
		if err := b.AddLink(link); err != nil {
			return err
		}
		return linkUp(link, netConf)
	}

	if netConf.IPV4LL {
		if err := AssignLinkLocalIP(link); err != nil {
			log.Errorf("IPV4LL set failed: %v", err)
			return err
		}
	} else {
		if err := RemoveLinkLocalIP(link); err != nil {
			log.Errorf("IPV4LL del failed: %v", err)
			return err
		}
	}

	addresses := []string{}

	if netConf.Address != "" {
		addresses = append(addresses, netConf.Address)
	}

	if len(netConf.Addresses) > 0 {
		addresses = append(addresses, netConf.Addresses...)
	}

	existingAddrs, _ := getLinkAddrs(link)
	addrMap := make(map[string]bool)
	for _, address := range addresses {
		addrMap[address] = true
	}
	for _, addr := range existingAddrs {
		if _, ok := addrMap[addr.IPNet.String()]; !ok {
			if netConf.DHCP || netConf.IPV4LL {
				// let the dhcpcd take care of it
				log.Infof("leaving  %s from %s", addr.String(), link.Attrs().Name)
			} else {
				log.Infof("removing  %s from %s", addr.String(), link.Attrs().Name)
				removeAddress(addr, link)
			}
		}
	}
	for _, address := range addresses {
		log.Infof("Applying %s to %s", address, link.Attrs().Name)
		err := applyAddress(address, link, netConf)
		if err != nil {
			log.Errorf("Failed to apply address %s to %s: %v", address, link.Attrs().Name, err)
		}
	}

	// TODO: can we set to default?
	if netConf.MTU > 0 {
		if err := netlink.LinkSetMTU(link, netConf.MTU); err != nil {
			log.Errorf("set MTU Failed: %v", err)
			return err
		}
	}

	if err := linkUp(link, netConf); err != nil {
		return err
	}

	// replace the existing gw with the main ipv4 one
	if err := setGateway(netConf.Gateway, true); err != nil {
		log.Errorf("Fail to set gateway %s", netConf.Gateway)
	}
	//and then add the ipv6 one if it exists
	if err := setGateway(netConf.GatewayIpv6, true); err != nil {
		log.Errorf("Fail to set gateway %s", netConf.GatewayIpv6)
	}

	// TODO: how to remove a GW? (on aws it seems to be hard to find out what the gw is :/)
	return nil
}

func runCmds(cmds []string, iface string) {
	log.Debugf("runCmds(on %s): %v", iface, cmds)
	for _, cmd := range cmds {
		log.Debugf("runCmd(on %s): %v", iface, cmd)
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}

		args, err := shlex.Split(strings.Replace(cmd, "$iface", iface, -1))
		if err != nil {
			log.Errorf("Failed to parse command [%s]: %v", cmd, err)
			continue
		}

		log.Infof("Running command %s %v", args[0], args[1:])
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Errorf("Failed to run command [%v]: %v", cmd, err)
			continue
		}
	}
}

func GetValidLinkList() ([]netlink.Link, error) {
	var validLinkList []netlink.Link
	links, err := netlink.LinkList()
	if err != nil {
		return validLinkList, err
	}

	for _, l := range links {
		linkName := l.Attrs().Name
		if linkName == "lo" || linkName == "docker-sys" || linkName == "docker0" {
			continue
		}
		validLinkList = append(validLinkList, l)
	}

	return validLinkList, nil
}

func StopDhcpcd() {
	cmd := exec.Command(exitDhcpArgs[0], exitDhcpArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Errorf("Failed to run command [%v]: %v", cmd, err)
	}
}

func StopWpaSupplicant() {
	links, err := GetValidLinkList()
	if err != nil {
		log.Errorf("error getting LinkList: %s", err)
		return
	}
	// need terminate all ifname
	for _, link := range links {
		cmd := exec.Command(exitWpaArgs[0], exitWpaArgs[1], "-i", link.Attrs().Name)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Errorf("Failed to run command %v: %v", cmd.Args, err)
		}
	}
}
