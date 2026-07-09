//go:build esp32 && wt32eth01

package wt32eth01

/*
#cgo LDFLAGS: -lesp_wifi -lesp_netif -lesp_event
#include <esp_wifi.h>
#include <esp_event.h>
#include <esp_netif.h>
#include <string.h>

static esp_netif_t* wifi_netif = NULL;

static int connect_wifi_sta(const char* ssid, const char* key, int use_dhcp, const char* ip, const char* netmask, const char* gw) {
    // 1. Initialize TCP/IP stack if not already done
    static int tcpip_inited = 0;
    if (!tcpip_inited) {
        esp_netif_init();
        esp_event_loop_create_default();
        tcpip_inited = 1;
    }

    // 2. Create WiFi Station netif
    if (!wifi_netif) {
        wifi_netif = esp_netif_create_default_wifi_sta();
    }

    // 3. Configure IP address (Static vs DHCP)
    if (!use_dhcp && ip && netmask && gw) {
        esp_netif_dhcpc_stop(wifi_netif);
        esp_netif_ip_info_t ip_info;
        ip_info.ip.addr = ipaddr_addr(ip);
        ip_info.netmask.addr = ipaddr_addr(netmask);
        ip_info.gw.addr = ipaddr_addr(gw);
        esp_netif_set_ip_info(wifi_netif, &ip_info);
    } else {
        esp_netif_dhcpc_start(wifi_netif);
    }

    // 4. Initialize WiFi
    wifi_init_config_t cfg = WIFI_INIT_CONFIG_DEFAULT();
    esp_wifi_init(&cfg);

    // 5. Configure WiFi STA mode
    wifi_config_t wifi_config;
    memset(&wifi_config, 0, sizeof(wifi_config));
    strcpy((char*)wifi_config.sta.ssid, ssid);
    strcpy((char*)wifi_config.sta.password, key);
    wifi_config.sta.threshold.authmode = WIFI_AUTH_WPA2_PSK;

    esp_wifi_set_mode(WIFI_MODE_STA);
    esp_wifi_set_config(WIFI_IF_STA, &wifi_config);

    // 6. Start WiFi
    if (esp_wifi_start() != ESP_OK) {
        return -1;
    }

    // 7. Connect
    if (esp_wifi_connect() != ESP_OK) {
        return -1;
    }

    return 0;
}

static int get_wifi_ip_address(char* out_ip, int max_len) {
    if (!wifi_netif) return -1;
    esp_netif_ip_info_t ip_info;
    if (esp_netif_get_ip_info(wifi_netif, &ip_info) != ESP_OK) {
        return -1;
    }
    // Convert to string
    unsigned char *ip_bytes = (unsigned char*)&ip_info.ip.addr;
    snprintf(out_ip, max_len, "%d.%d.%d.%d", ip_bytes[0], ip_bytes[1], ip_bytes[2], ip_bytes[3]);
    return 0;
}
*/
import "C"
import (
	"errors"
	"time"
)

type WiFi struct {
	ssid string
	key  string
}

func NewWiFi(ssid, key string) *WiFi {
	return &WiFi{
		ssid: ssid,
		key:  key,
	}
}

// Connect joins the WiFi network.
func (w *WiFi) Connect(dhcp bool, ip, netmask, gateway string) error {
	cSSID := C.CString(w.ssid)
	cKey := C.CString(w.key)
	defer C.free(unsafePointer(cSSID))
	defer C.free(unsafePointer(cKey))

	var cIP, cNetmask, cGateway *C.char
	useDHCP := 1
	if !dhcp {
		useDHCP = 0
		cIP = C.CString(ip)
		cNetmask = C.CString(netmask)
		cGateway = C.CString(gateway)
		defer C.free(unsafePointer(cIP))
		defer C.free(unsafePointer(cNetmask))
		defer C.free(unsafePointer(cGateway))
	}

	res := C.connect_wifi_sta(cSSID, cKey, C.int(useDHCP), cIP, cNetmask, cGateway)
	if res != 0 {
		return errors.New("wifi: failed to initiate WiFi connection")
	}

	// Wait up to 10 seconds for connection to succeed
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		ipStr, err := w.GetIP()
		if err == nil && ipStr != "0.0.0.0" && ipStr != "" {
			return nil
		}
	}

	return errors.New("wifi: connection timeout")
}

// GetIP returns the current IP address of the WiFi interface.
func (w *WiFi) GetIP() (string, error) {
	var buf [32]C.char
	res := C.get_wifi_ip_address(&buf[0], 32)
	if res != 0 {
		return "", errors.New("wifi: failed to get IP address")
	}
	return C.GoString(&buf[0]), nil
}

func unsafePointer(p *C.char) unsafe.Pointer {
	return unsafe.Pointer(p)
}
